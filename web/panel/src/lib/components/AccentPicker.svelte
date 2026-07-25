<script lang="ts">
  import { onMount } from "svelte";
  import { ACCENT_PRESETS, applyAccentPreset, getStoredAccentPreset, type AccentPresetId } from "../accent";

  let selected: AccentPresetId = $state("wayang");

  onMount(() => {
    selected = getStoredAccentPreset();
  });

  function select(id: AccentPresetId) {
    selected = id;
    applyAccentPreset(id);
  }
</script>

<!--
  Presets swap only --color-accent/--color-accent-hover/--color-accent-soft/
  --color-accent-contrast (§13.3) - warning/danger/success tokens are never
  touched here, so status colors stay stable regardless of accent choice.
-->
<div class="presets" role="radiogroup" aria-label="Accent color">
  {#each ACCENT_PRESETS as preset (preset.id)}
    <button
      type="button"
      role="radio"
      aria-checked={selected === preset.id}
      class="preset"
      class:active={selected === preset.id}
      onclick={() => select(preset.id)}
      title={preset.label}
    >
      <span class="swatch" style:background={preset.light["--color-accent"]} aria-hidden="true"></span>
      <span class="label">{preset.label}</span>
    </button>
  {/each}
</div>

<style>
  .presets {
    display: grid;
    grid-template-columns: repeat(3, minmax(105px, 1fr));
    gap: 0.55rem;
  }
  .preset {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    border: 1px solid var(--color-border);
    background: color-mix(in srgb, var(--color-surface-raised) 88%, transparent);
    color: var(--color-text);
    border-radius: 10px;
    padding: 0.55rem 0.65rem;
    min-height: 48px;
    cursor: pointer;
    font-size: 0.78rem;
    font-weight: 600;
    box-shadow: 0 1px 2px color-mix(in srgb, var(--color-text) 5%, transparent);
  }
  .preset.active {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-accent) 12%, transparent);
  }
  .preset.active::after {
    content: "✓";
    margin-left: auto;
    color: var(--color-accent);
    font-size: 0.72rem;
    font-weight: 800;
  }
  .preset:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .swatch {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 2px solid var(--color-surface-raised);
    box-shadow: 0 0 0 1px var(--color-border-strong);
    flex-shrink: 0;
  }

  @media (max-width: 520px) {
    .presets {
      grid-template-columns: repeat(2, minmax(105px, 1fr));
    }
  }

  @media (prefers-reduced-motion: no-preference) {
    .preset {
      transition: border-color 120ms ease, background-color 120ms ease, box-shadow 120ms ease;
    }
  }
</style>
