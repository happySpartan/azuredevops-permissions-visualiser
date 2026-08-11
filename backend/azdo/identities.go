package azdo

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// identityResolve. returns the Graph descriptor for a general descriptor.
//
// Azure DevOps uses two descriptor encodings. The Graph API identifies subjects
// with descriptors like "vssgp.Uy0x...", "msa.<...>", "svc.<...>"; the security
// ACL APIs use general (storage) descriptors like
// "Microsoft.TeamFoundation.Identity;S-1-9-...". They are not interchangeable:
// the two forms can encode different values for the same identity (for example
// the SID in a general descriptor does not always match the SID a vssgp
// descriptor base64-encodes). The identity API is the authoritative translator.
//
// ResolvedIdentity carries the Graph identity metadata returned by the legacy
// identity API. That API includes built-in collection groups which may be
// omitted from the Graph groups listing even though they own ACL entries.
type ResolvedIdentity struct {
	Descriptor  string
	DisplayName string
	Origin      string
	SubjectKind string
}

// IdentityGraphDescriptors resolves each general descriptor to its Graph
// identity via the /_apis/Identities endpoint and returns a map. Descriptors
// that cannot be resolved are omitted from the result. Requests are batched to
// stay within the API's per-call descriptor limit.
func (c *Client) IdentityGraphDescriptors(ctx context.Context, general []string) (map[string]ResolvedIdentity, error) {
	out := make(map[string]ResolvedIdentity)
	// Deduplicate and drop anything that is already a Graph descriptor.
	seen := make(map[string]bool)
	var need []string
	for _, d := range general {
		if d == "" || isGraphDescriptor(d) {
			continue
		}
		if !seen[d] {
			seen[d] = true
			need = append(need, d)
		}
	}
	// The identity API accepts descriptors as a comma-separated list, but the
	// whole query string is subject to a URL-length cap (observed ~5.1KB
	// against a live org: larger batches 404). Chunk so each request stays
	// comfortably under that bound.
	const maxQueryLen = 4000
	for len(need) > 0 {
		batch := need[0:1]
		need = need[1:]
		for len(need) > 0 && queryLen(batch, need[0]) <= maxQueryLen {
			batch = append(batch, need[0])
			need = need[1:]
		}
		resolved, err := c.identityGraphDescriptorsBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		for general, identity := range resolved {
			out[general] = identity
		}
	}
	return out, nil
}

// queryLen reports the approximate URL query length of descriptors joined onto
// the existing batch, encoded as the identity API expects (commas URL-encoded).
func queryLen(batch []string, extra string) int {
	n := len(batch)
	for _, d := range batch {
		n += len(url.QueryEscape(d))
	}
	if extra != "" {
		n += len(url.QueryEscape(extra))
	}
	return n
}

func (c *Client) identityGraphDescriptorsBatch(ctx context.Context, batch []string) (map[string]ResolvedIdentity, error) {
	var out struct {
		Value []struct {
			Descriptor          string `json:"descriptor"` // general descriptor
			SubjectDescriptor   string `json:"subjectDescriptor"`
			ProviderDisplayName string `json:"providerDisplayName"`
			CustomDisplayName   string `json:"customDisplayName"`
			IsContainer         bool   `json:"isContainer"`
		} `json:"value"`
	}
	q := url.Values{"api-version": {"7.1-preview.1"}}
	q.Set("descriptors", strings.Join(batch, ","))
	if err := c.vsspsGet(ctx, "/_apis/Identities", q, &out); err != nil {
		return nil, fmt.Errorf("azdo: identity resolution: %w", err)
	}
	resolved := make(map[string]ResolvedIdentity, len(out.Value))
	for i, idn := range out.Value {
		general := idn.Descriptor
		// The endpoint preserves request order but can canonicalize storage
		// descriptors in its response (observed for built-in collection groups).
		// Keep the requested descriptor as the lookup key in that case so callers
		// can translate the exact descriptor present in the ACL.
		if i < len(batch) && general != batch[i] {
			general = batch[i]
		}
		if general == "" || idn.SubjectDescriptor == "" {
			continue
		}
		displayName := idn.CustomDisplayName
		if displayName == "" {
			displayName = idn.ProviderDisplayName
		}
		kind := "user"
		if idn.IsContainer {
			kind = "group"
		}
		resolved[general] = ResolvedIdentity{
			Descriptor:  idn.SubjectDescriptor,
			DisplayName: displayName,
			Origin:      graphDescriptorOrigin(idn.SubjectDescriptor),
			SubjectKind: kind,
		}
	}
	return resolved, nil
}

func graphDescriptorOrigin(descriptor string) string {
	if i := strings.IndexByte(descriptor, '.'); i > 0 {
		return descriptor[:i]
	}
	return ""
}

// isGraphDescriptor reports whether d already uses the Graph descriptor prefix.
func isGraphDescriptor(d string) bool {
	for _, p := range []string{"vssgp.", "vssd.", "msa.", "aad.", "svc.", "vstu.", "azureid."} {
		if strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}
