<script lang="ts">
  import { getPath, navigate } from "../router/router.svelte";
  import ThemeToggle from "./ThemeToggle.svelte";
  import Icon, { type IconName } from "./Icon.svelte";

  const links: { path: string; label: string; icon: IconName; disabled?: boolean }[] = [
    { path: "/", label: "Overview", icon: "dashboard" },
    { path: "/projects", label: "Projects", icon: "folder" },
    { path: "/deliveries", label: "Deliveries", icon: "git-branch" },
    { path: "/improvements", label: "Context Improvements", icon: "comment" },
    { path: "/system", label: "System", icon: "settings" },
  ];

  function isActive(path: string): boolean {
    const current = getPath();
    if (path === "/") return current === "/";
    return current.startsWith(path);
  }
</script>

<nav aria-label="Primary">
  <a class="brand" href="/" onclick={(e) => { e.preventDefault(); navigate("/"); }}>
    <span class="brand-mark">
      <img class="brand-logo" src="/logo.svg" alt="" aria-hidden="true" width="32" height="32" />
    </span>
    <span class="brand-copy">
      <span class="brand-name">Punakawan</span>
      <span class="brand-tagline">Agent operations</span>
    </span>
  </a>
  <span class="section-label">Workspace</span>
  <ul>
    {#each links as link (link.path)}
      <li>
        {#if link.disabled}
          <span class="link disabled" title="Not implemented yet"><Icon name={link.icon} />{link.label}</span>
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
            <Icon name={link.icon} />
            {link.label}
          </a>
        {/if}
      </li>
    {/each}
  </ul>

  <div class="footer">
    <span class="footer-label">Appearance</span>
    <ThemeToggle />
  </div>
</nav>

<style>
  nav {
    width: 248px;
    flex-shrink: 0;
    box-sizing: border-box;
    height: 100%;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--color-border);
    padding: 1.1rem 0.75rem;
    background: color-mix(in srgb, var(--color-surface) 94%, transparent);
    backdrop-filter: blur(16px);
    box-shadow: 8px 0 28px rgb(16 24 40 / 0.035);
  }
  .footer {
    margin-top: auto;
    padding: 0.75rem 0.6rem 0.25rem;
    border-top: 1px solid var(--color-border);
    display: grid;
    gap: 0.4rem;
  }
  .footer-label {
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-muted);
  }
  .footer :global(.segmented) {
    width: 100%;
    justify-content: space-between;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.7rem;
    padding: 0.15rem 0.45rem 1rem;
    margin-bottom: 0.85rem;
    text-decoration: none;
    color: inherit;
    border-bottom: 1px solid var(--color-border);
  }
  .brand-name {
    font-size: 1.02rem;
    font-weight: 750;
    letter-spacing: 0.01em;
  }
  .brand-mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 42px;
    height: 42px;
    border-radius: 12px;
    background: linear-gradient(145deg, var(--color-gold-soft), var(--color-accent-soft));
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--color-gold) 20%, transparent);
  }
  .brand-copy {
    display: grid;
    line-height: 1.2;
  }
  .brand-tagline {
    color: var(--color-text-muted);
    font-size: 0.7rem;
    margin-top: 0.15rem;
  }
  .section-label {
    display: block;
    margin: 0 0.55rem 0.45rem;
    color: var(--color-text-muted);
    font-size: 0.68rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
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
    display: flex;
    align-items: center;
    gap: 0.65rem;
    padding: 0.62rem 0.7rem;
    border-radius: 9px;
    text-decoration: none;
    color: var(--color-text);
  }
  .link:hover {
    background: var(--color-surface-subtle);
    color: var(--color-accent);
  }
  .link.active {
    background: var(--color-accent-soft);
    color: var(--color-accent);
    font-weight: 600;
    box-shadow: inset 3px 0 0 var(--color-accent);
  }
  .link.disabled {
    color: var(--color-text-muted);
    cursor: default;
  }

  @media (max-width: 639px) {
    nav {
      width: 100%;
      border-right: none;
      border-bottom: 1px solid var(--color-border);
      padding: 0.5rem;
    }
    ul {
      grid-auto-flow: column;
      grid-auto-columns: max-content;
      overflow-x: auto;
    }
  }
</style>
