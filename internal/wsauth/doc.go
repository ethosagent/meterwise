// Package wsauth is the data-plane authenticator: the workspace URL is the credential. It reads
// /w/{publicId}/... from the request path, resolves the id through the workspace directory and
// answers with a tollgate Tenant. It never reads, stores or logs Authorization or x-api-key (they
// pass through to the provider untouched) and adds no header of its own. Plan 05 mounts it.
package wsauth
