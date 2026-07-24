<script lang="ts">
  import { onMount } from "svelte";
  import { getPath, navigate } from "../router/router.svelte";
  import { applyTheme, getStoredThemePreference, type ThemePreference } from "../theme";
  import { reapplyStoredAccent } from "../accent";

  // Mirrors Sidebar's top-level links (UI-005). Kept as a separate literal
  // rather than importing Sidebar's list, since Sidebar has no exported
  // links constant and duplicating four short entries is cheaper than
  // introducing a shared module for it right now.
  const links: { path: string; label: string; icon: string }[] = [
    { path: "/", label: "Overview", icon: "⌂" },
    { path: "/projects", label: "Projects", icon: "❖" },
    { path: "/system", label: "System", icon: "⚙" },
  ];

  function isActive(path: string): boolean {
    const current = getPath();
    if (path === "/") return current === "/";
    return current.startsWith(path);
  }

  // The sidebar (with its ThemeToggle) is hidden below 640px, so the theme
  // control lives here too. A single tap cycles System -> Light -> Dark and
  // reuses the same theme.ts persistence + accent re-apply as ThemeToggle.
  const order: ThemePreference[] = ["system", "light", "dark"];
  const themeIcons: Record<ThemePreference, string> = { system: "◐", light: "☀", dark: "☾" };
  const themeLabels: Record<ThemePreference, string> = { system: "System", light: "Light", dark: "Dark" };
  let theme: ThemePreference = $state("system");

  onMount(() => {
    theme = getStoredThemePreference();
  });

  function cycleTheme() {
    const next = order[(order.indexOf(theme) + 1) % order.length];
    theme = next;
    applyTheme(next);
    reapplyStoredAccent();
  }
</script>

<!--
  Bottom tab bar shown only below 640px (§13.4), replacing Sidebar's
  reflow for the mobile range. AppShell always renders this; it is
  invisible above the breakpoint via the media query below.
-->
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
      <span class="icon" aria-hidden="true">{link.icon}</span>
      <span class="label">{link.label}</span>
    </a>
  {/each}
  <button
    type="button"
    class="tab theme-tab"
    onclick={cycleTheme}
    aria-label={`Theme: ${themeLabels[theme]}. Tap to change.`}
    title={`Theme: ${themeLabels[theme]}`}
  >
    <span class="icon" aria-hidden="true">{themeIcons[theme]}</span>
    <span class="label">{themeLabels[theme]}</span>
  </button>
</nav>

<style>
  .mobile-nav {
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
      font-size: 1.1rem;
    }
  }
</style>
