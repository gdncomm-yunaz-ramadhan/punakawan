<script lang="ts">
  import type { Snippet } from "svelte";
  import Sidebar from "./Sidebar.svelte";
  import MobileNavigation from "./MobileNavigation.svelte";
  import TopBar from "./TopBar.svelte";
  import type { SystemInfo } from "../api/client";

  interface Props {
    system: SystemInfo | null;
    children: Snippet;
  }
  let { system, children }: Props = $props();
</script>

<!--
  Extracted from App.svelte's former inline .shell/.content-area markup
  (UI-005). Sidebar collapses into MobileNavigation's bottom tab bar
  below 640px (§13.4's mobile breakpoint), rather than Sidebar's old
  720px horizontal-scroll fallback.
-->
<div class="shell">
  <div class="sidebar-slot">
    <Sidebar />
  </div>
  <div class="content-area">
    <TopBar {system} />
    <main>
      {@render children()}
    </main>
  </div>
  <MobileNavigation />
</div>

<style>
  .shell {
    display: flex;
    min-height: 100vh;
    background: var(--color-bg);
    color: var(--color-text);
  }
  .content-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  main {
    padding: 1rem 1.5rem;
    max-width: 1100px;
    width: 100%;
    box-sizing: border-box;
  }

  @media (max-width: 639px) {
    .shell {
      flex-direction: column;
      /* Leave room for MobileNavigation's fixed bottom tab bar. */
      padding-bottom: 56px;
    }
    .sidebar-slot {
      display: none;
    }
    main {
      padding: 0.75rem 1rem;
    }
  }
</style>
