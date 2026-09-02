<script lang="ts">
  import { getPath, navigate } from "../router/router.svelte";
  import Icon, { type IconName } from "./Icon.svelte";
  import ThemeToggle from "./ThemeToggle.svelte";

  // Mirrors Sidebar's top-level links (UI-005). Kept as a separate literal
  // rather than importing Sidebar's list, since Sidebar has no exported
  // links constant and duplicating four short entries is cheaper than
  // introducing a shared module for it right now.
  const links: { path: string; label: string; icon: IconName }[] = [
    { path: "/projects", label: "Projects", icon: "folder" },
    { path: "/deliveries", label: "Deliveries", icon: "git-branch" },
    { path: "/connectors", label: "Connectors", icon: "server" },
    { path: "/settings", label: "Settings", icon: "settings" },
  ];

  function isActive(path: string): boolean {
    const current = getPath();
    if (path === "/projects") return current === "/" || current.startsWith(path);
    return current.startsWith(path);
  }

  // The sidebar (with its ThemeToggle) is hidden below 640px, so the theme
  // control lives here too. Rather than a tab whose label cycles through
  // System/Light/Dark - which collided with the "System" page link, showing
  // two "System" entries - this is a single fixed "Theme" button that opens a
  // popover with the same ThemeToggle segmented control the sidebar uses.
  let themeOpen = $state(false);

  function closeTheme() {
    themeOpen = false;
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") closeTheme();
  }
</script>

<svelte:window onkeydown={themeOpen ? onKeydown : undefined} />

<!--
  Bottom tab bar shown only below 640px (§13.4), replacing Sidebar's
  reflow for the mobile range. AppShell always renders this; it is
  invisible above the breakpoint via the media query below.
-->
{#if themeOpen}
  <!-- Dismiss layer: a tap anywhere outside the popover closes it. -->
  <button type="button" class="theme-scrim" aria-label="Close theme menu" onclick={closeTheme}></button>
  <div class="theme-popover" role="dialog" aria-label="Theme" aria-modal="true">
    <span class="theme-popover-title">Theme</span>
    <ThemeToggle onselect={closeTheme} />
  </div>
{/if}

<nav class="mobile-nav" aria-label="Primary">
  {#each links as link (link.path)}
    <a
      href={link.path}
      class="tab"
      class:active={isActive(link.path)}
      onclick={(e) => {
        e.preventDefault();
        navigate(link.path);
      }}
    >
      <span class="icon" aria-hidden="true"><Icon name={link.icon} size={20} /></span>
      <span class="label">{link.label}</span>
    </a>
  {/each}
  <button
    type="button"
    class="tab theme-tab"
    class:active={themeOpen}
    onclick={() => (themeOpen = !themeOpen)}
    aria-haspopup="dialog"
    aria-expanded={themeOpen}
    aria-label="Theme"
  >
    <span class="icon" aria-hidden="true"><Icon name="palette" size={20} /></span>
    <span class="label">Theme</span>
  </button>
</nav>

<style>
  .mobile-nav {
    display: none;
  }
  .theme-scrim,
  .theme-popover {
    display: none;
  }

  @media (max-width: 639px) {
    .mobile-nav {
      display: flex;
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 20;
      background: var(--color-surface-raised);
      border-top: 1px solid var(--color-border);
      padding-bottom: env(safe-area-inset-bottom, 0);
    }
    .tab {
      flex: 1;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 0.15rem;
      min-height: 48px;
      padding: 0.3rem 0.2rem;
      text-decoration: none;
      color: var(--color-text-muted);
      font-size: 0.7rem;
    }
    .tab.active {
      color: var(--color-accent);
      font-weight: 600;
    }
    .theme-tab {
      border: none;
      background: transparent;
      cursor: pointer;
      font-family: inherit;
    }
    .icon {
      display: inline-flex;
      line-height: 1;
    }

    /* Full-viewport dismiss layer beneath the popover. */
    .theme-scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 25;
      border: none;
      padding: 0;
      background: color-mix(in srgb, var(--color-text) 28%, transparent);
    }
    /* Bottom sheet sitting just above the tab bar. */
    .theme-popover {
      display: flex;
      flex-direction: column;
      gap: 0.6rem;
      position: fixed;
      left: 0.75rem;
      right: 0.75rem;
      bottom: calc(56px + env(safe-area-inset-bottom, 0px) + 0.5rem);
      z-index: 26;
      padding: 0.9rem;
      border: 1px solid var(--color-border);
      border-radius: var(--radius-card);
      background: var(--color-surface-raised);
      box-shadow: var(--shadow-lg);
    }
    .theme-popover-title {
      font-size: 0.72rem;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--color-text-muted);
    }
  }
</style>
