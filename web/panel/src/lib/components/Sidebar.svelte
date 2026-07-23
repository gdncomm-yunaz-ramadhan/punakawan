<script lang="ts">
  import { getPath, navigate } from "../router/router.svelte";

  const links: { path: string; label: string; disabled?: boolean }[] = [
    { path: "/", label: "Overview" },
    { path: "/workspaces", label: "Workspaces" },
    { path: "/search", label: "Search" },
    { path: "/system", label: "System" },
  ];

  function isActive(path: string): boolean {
    const current = getPath();
    if (path === "/") return current === "/";
    return current.startsWith(path);
  }
</script>

<nav aria-label="Primary">
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

  @media (max-width: 720px) {
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
