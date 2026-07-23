<script lang="ts">
  import { getPath, navigate } from "../router/router.svelte";

  const links: { path: string; label: string; disabled?: boolean }[] = [
    { path: "/", label: "Overview" },
    { path: "/workspaces", label: "Workspaces" },
    { path: "/search", label: "Search" },
    { path: "/system", label: "System" },
    { path: "/showcase", label: "Showcase" },
  ];

  function isActive(path: string): boolean {
    const current = getPath();
    if (path === "/") return current === "/";
    return current.startsWith(path);
  }
</script>

<nav aria-label="Primary">
  <a class="brand" href="/" onclick={(e) => { e.preventDefault(); navigate("/"); }}>
    <img class="brand-logo" src="/logo.svg" alt="" aria-hidden="true" width="32" height="32" />
    <span class="brand-name">Punakawan</span>
  </a>
  <ul>
    {#each links as link (link.path)}
      <li>
        {#if link.disabled}
          <span class="link disabled" title="Not implemented yet">{link.label}</span>
        {:else}
          <a
            href={link.path}
            class="link"
            class:active={isActive(link.path)}
            onclick={(e) => {
              e.preventDefault();
              navigate(link.path);
            }}
          >
            {link.label}
          </a>
        {/if}
      </li>
    {/each}
  </ul>
</nav>

<style>
  nav {
    width: 220px;
    flex-shrink: 0;
    border-right: 1px solid #e0e0e0;
    padding: 1rem 0.5rem;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.25rem 0.5rem 0.75rem;
    margin-bottom: 0.5rem;
    text-decoration: none;
    color: inherit;
    border-bottom: 1px solid #e0e0e0;
  }
  .brand-name {
    font-size: 1.05rem;
    font-weight: 700;
    letter-spacing: 0.01em;
  }
  /* Logo art is a monochrome black silhouette; invert it in dark mode so it
     reads as light-on-dark. data-theme lives on <html> (see index.html). */
  :global(html[data-theme="dark"]) .brand-logo {
    filter: invert(1);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.25rem;
  }
  .link {
    display: block;
    padding: 0.4rem 0.6rem;
    border-radius: 6px;
    text-decoration: none;
    color: #333;
  }
  .link:hover {
    background: #f0f0f0;
  }
  .link.active {
    background: #e8eaf6;
    color: #3949ab;
    font-weight: 600;
  }
  .link.disabled {
    color: #aaa;
    cursor: default;
  }

  @media (max-width: 639px) {
    nav {
      width: 100%;
      border-right: none;
      border-bottom: 1px solid #e0e0e0;
      padding: 0.5rem;
    }
    ul {
      grid-auto-flow: column;
      grid-auto-columns: max-content;
      overflow-x: auto;
    }
  }
</style>
