// Package tenancy holds the rules of the one organization (decision D4): who is
// a member, how an invitation becomes a membership, how many workspaces exist,
// the unguessable workspace public id and the cached WorkspaceDirectory the data
// plane reads. It owns no SQL; the store package does. Every method takes the
// calling principal or the org id, so a query is always scoped (TNT-1).
package tenancy
