// Package access is the RBAC kernel: the permission taxonomy (the only definition of what can be
// granted), the pure PermissionEvaluator, the GrantRepository (the only I/O of authorization),
// the Authorizer the API gateway calls, and the group and org-default use cases. It imports
// internal/contract only (the store reaches it through the Store interface); the HTTP layer
// never decides a permission itself.
package access
