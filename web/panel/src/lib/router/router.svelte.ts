// A minimal History-API router. The panel deliberately has few routes
// (§13's information architecture is shallow), so a full router library
// is not worth the dependency - this is ~30 real lines, not a stub.

let path = $state(currentPath());

function currentPath(): string {
  return typeof window === "undefined" ? "/" : window.location.pathname;
}

export function getPath(): string {
  return path;
}

export function navigate(to: string): void {
  if (typeof window !== "undefined") {
    window.history.pushState({}, "", to);
  }
  path = to;
}

if (typeof window !== "undefined") {
  window.addEventListener("popstate", () => {
    path = currentPath();
  });
}
