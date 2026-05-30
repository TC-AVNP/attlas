// Detects whether petboard is served path-based (attlas.uk/petboard/)
// or on its own subdomain (petboard.attlas.uk/). The Go server handles
// both, but the frontend needs to know which prefix to use for API
// calls and React Router's basename.
//
// Path-based: basename="/petboard", API="/petboard/api"
// Subdomain:  basename="/",        API="/api"

const isSubdomain = window.location.host.startsWith("petboard.");

export const routerBasename = isSubdomain ? "/" : "/petboard";
export const apiPrefix = isSubdomain ? "/api" : "/petboard/api";
