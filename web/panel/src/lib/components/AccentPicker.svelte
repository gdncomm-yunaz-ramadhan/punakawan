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
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .preset {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    border: 1px solid var(--color-border);
    background: var(--color-surface-subtle);
    color: var(--color-text);
    border-radius: 8px;
    padding: 0.35rem 0.6rem;
    min-height: 44px;
    cursor: pointer;
    font-size: 0.85rem;
  }
  .preset.active {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
    font-weight: 600;
  }
  .swatch {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 1px solid var(--color-border-strong);
    flex-shrink: 0;
  }
</style>
