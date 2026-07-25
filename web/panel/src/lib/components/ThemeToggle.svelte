<script lang="ts">
  import { onMount } from "svelte";
  import { applyTheme, getStoredThemePreference, type ThemePreference } from "../theme";
  import { reapplyStoredAccent } from "../accent";
  import Icon, { type IconName } from "./Icon.svelte";

  // onselect fires after a preference is applied, letting a caller (e.g. the
  // mobile theme popover) dismiss itself once the user picks. Optional, so the
  // sidebar's inline usage is unaffected.
  interface Props {
    onselect?: (pref: ThemePreference) => void;
  }
  let { onselect }: Props = $props();

  let selected: ThemePreference = $state("system");

  onMount(() => {
    selected = getStoredThemePreference();
  });

  const options: { id: ThemePreference; label: string; icon: IconName }[] = [
    { id: "light", label: "Light", icon: "sun" },
    { id: "dark", label: "Dark", icon: "moon" },
    { id: "system", label: "System", icon: "monitor" },
  ];

  function select(pref: ThemePreference) {
    selected = pref;
    applyTheme(pref);
    // The stored accent preset has distinct light/dark hex pairs, so
    // switching the resolved theme needs to re-apply it.
    reapplyStoredAccent();
    onselect?.(pref);
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
      <Icon name={opt.icon} size={14} strokeWidth={2} />
      <span>{opt.label}</span>
    </button>
  {/each}
</div>

<style>
  .segmented {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 3px;
    width: 100%;
    padding: 3px;
    border: 1px solid var(--color-border);
    border-radius: 10px;
    background: color-mix(in srgb, var(--color-surface-subtle) 88%, transparent);
    box-shadow: inset 0 1px 1px color-mix(in srgb, var(--color-text) 5%, transparent);
  }
  .segment {
    border: none;
    background: transparent;
    color: var(--color-text-muted);
    font-size: 0.72rem;
    font-weight: 600;
    padding: 0.45rem 0.25rem;
    border-radius: 7px;
    cursor: pointer;
    min-height: 34px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.3rem;
  }
  .segment.active {
    background: var(--color-surface-raised);
    color: var(--color-text);
    box-shadow: 0 1px 3px color-mix(in srgb, var(--color-text) 15%, transparent);
  }
  .segment:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: no-preference) {
    .segment {
      transition: background-color 120ms ease, color 120ms ease;
    }
  }
</style>
