package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// GroupMembershipDetail is the selected group and all subjects reachable from
// its collected direct membership edges.
type GroupMembershipDetail struct {
	Group   Subject       `json:"group"`
	Members []GroupMember `json:"members"`
}

// GroupMember describes a direct or transitive group member and one shortest
// membership path from the selected group to that subject.
type GroupMember struct {
	Subject Subject   `json:"subject"`
	Direct  bool      `json:"direct"`
	Path    []Subject `json:"path"`
}

// GroupMembershipByRun returns direct and transitive members of a collected
// group. Nested groups remain members in their own right.
func (s *Store) GroupMembershipByRun(ctx context.Context, runID int64, descriptor string) (*GroupMembershipDetail, error) {
	subjects, err := subjectsForMembership(ctx, s.db, runID)
	if err != nil {
		return nil, err
	}
	group, ok := subjects[descriptor]
	if !ok || group.Kind != "group" {
		return nil, ErrNotFound
	}

	edges := map[string][]string{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT parent_descriptor, member_descriptor
		FROM memberships WHERE run_id=?
		ORDER BY parent_descriptor, member_descriptor`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: group memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var parent, member string
		if err := rows.Scan(&parent, &member); err != nil {
			return nil, err
		}
		edges[parent] = append(edges[parent], member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for parent := range edges {
		sort.Slice(edges[parent], func(i, j int) bool {
			left, right := subjects[edges[parent][i]], subjects[edges[parent][j]]
			if left.DisplayName != right.DisplayName {
				return left.DisplayName < right.DisplayName
			}
			return left.Descriptor < right.Descriptor
		})
	}

	paths := map[string][]string{descriptor: {descriptor}}
	queue := []string{descriptor}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, member := range edges[parent] {
			if _, known := paths[member]; known {
				continue
			}
			if _, known := subjects[member]; !known {
				continue
			}
			paths[member] = append(append([]string{}, paths[parent]...), member)
			queue = append(queue, member)
		}
	}
	delete(paths, descriptor)

	members := make([]GroupMember, 0, len(paths))
	for memberDescriptor, pathDescriptors := range paths {
		path := make([]Subject, 0, len(pathDescriptors))
		for _, pathDescriptor := range pathDescriptors {
			path = append(path, subjects[pathDescriptor])
		}
		members = append(members, GroupMember{
			Subject: subjects[memberDescriptor],
			Direct:  len(pathDescriptors) == 2,
			Path:    path,
		})
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Subject.DisplayName != members[j].Subject.DisplayName {
			return members[i].Subject.DisplayName < members[j].Subject.DisplayName
		}
		return members[i].Subject.Descriptor < members[j].Subject.Descriptor
	})
	return &GroupMembershipDetail{Group: group, Members: members}, nil
}

func subjectsForMembership(ctx context.Context, db *sql.DB, runID int64) (map[string]Subject, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT descriptor, display_name, origin, subject_kind
		FROM subjects WHERE run_id=?`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: membership subjects: %w", err)
	}
	defer rows.Close()
	subjects := map[string]Subject{}
	for rows.Next() {
		var subject Subject
		if err := rows.Scan(&subject.Descriptor, &subject.DisplayName, &subject.Origin, &subject.Kind); err != nil {
			return nil, err
		}
		subjects[subject.Descriptor] = subject
	}
	return subjects, rows.Err()
}
