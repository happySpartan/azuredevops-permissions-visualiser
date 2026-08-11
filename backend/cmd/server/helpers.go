package main

import (
	"context"
	"errors"
	"os"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/azdo"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/collect"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
)

// errNoOrg indicates AZDO_ORG is not configured.
var errNoOrg = errors.New("AZDO_ORG is not set; set it to your Azure DevOps organization name (for example AZDO_ORG=my-org) and restart the visualiser")

// requireAzAuth verifies the Azure CLI is present and can yield a token,
// returning a descriptive error if not. It is a lightweight probe so the API
// returns a clear message instead of a generic failure mid-collection.
func requireAzAuth(ctx context.Context) error {
	provider := azdo.NewAzCLITokenProvider()
	_, err := provider.Token(ctx)
	return err
}

// newCollector wires the azdo client and store into a collector.
func newCollector(client *azdo.Client, st *store.Store) *collect.Collector {
	return collect.New(client, st)
}

// ensureDataDir creates the store data directory if missing.
func ensureDataDir() error {
	return os.MkdirAll(store.DefaultDataDir(), 0o755)
}
