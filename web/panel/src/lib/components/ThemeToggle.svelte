<script lang="ts">
  import { onMount } from "svelte";
  import { applyTheme, getStoredThemePreference, type ThemePreference } from "../theme";
  import { reapplyStoredAccent } from "../accent";

  let selected: ThemePreference = $state("system");

  onMount(() => {
    selected = getStoredThemePreference();
  });

  const options: { id: ThemePreference; label: string }[] = [
    { id: "light", label: "Light" },
    { id: "dark", label: "Dark" },
    { id: "system", label: "System" },
  ];

  function select(pref: ThemePreference) {
    selected = pref;
    applyTheme(pref);
    // The stored accent preset has distinct light/dark hex pairs, so
    // switching the resolved theme needs to re-apply it.
    reapplyStoredAccent();
  }
</script>

<!--
  A segmented control, not an animated switch - per §13.3, theme changes
  must respect prefers-reduced-motion, so this intentionally has no
  elaborate transition; the only motion is a plain background-color swap
  on the active segment.
-->
<div class="segmented" role="radiogroup" aria-label="Theme">
  {#each options as opt (opt.id)}
    <button
      type="button"
      role="radio"
      aria-checked={selected === opt.id}
      class="segment"
      class:active={selected === opt.id}
      onclick={() => select(opt.id)}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .segmented {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: var(--color-surface-subtle);
  }
  .segment {
    border: none;
    background: transparent;
    color: var(--color-text-muted);
    font-size: 0.85rem;
    padding: 0.35rem 0.75rem;
    border-radius: 6px;
    cursor: pointer;
    min-height: 32px;
  }
  .segment.active {
    background: var(--color-surface-raised);
    color: var(--color-text);
    font-weight: 600;
    box-shadow: var(--shadow-card);
  }

  @media (prefers-reduced-motion: no-preference) {
    .segment {
      transition: background-color 120ms ease, color 120ms ease;
    }
  }
</style>
