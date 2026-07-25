<script lang="ts">
  import { onMount } from "svelte";
  import {
    getRoles,
    updateRole,
    resetRole,
    ApiError,
    type RoleConfig,
    type RolesConfiguration,
    type RoleCapabilityInfo,
  } from "../../lib/api/client";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  // The four roles, in fixed presentation order.
  type RoleName = "semar" | "gareng" | "petruk" | "bagong";
  const ROLE_ORDER: RoleName[] = ["semar", "gareng", "petruk", "bagong"];

  // Display name + one-line responsibility for each role (plan §8-11).
  const ROLE_META: Record<RoleName, { label: string; responsibility: string }> = {
    semar: {
      label: "Semar",
      responsibility:
        "Coordinates: interprets intent, selects workflows, manages the change dossier, creates handoff capsules.",
    },
    gareng: {
      label: "Gareng",
      responsibility:
        "Challenges: finds missing context, detects contradictions, analyzes cross-repository impact and risk.",
    },
    petruk: {
      label: "Petruk",
      responsibility:
        "Plans and builds: proposes solutions, decomposes tasks, implements accepted work across repositories.",
    },
    bagong: {
      label: "Bagong",
      responsibility: "Verifies: independently checks plan conformance, tests, coverage, and dossier claims.",
    },
  };

  const STYLES = [
    { id: "strict", label: "Strict" },
    { id: "balanced", label: "Balanced" },
    { id: "creative", label: "Creative" },
  ];
  const MODES = [
    { id: "assist", label: "Assist" },
    { id: "propose", label: "Propose" },
    { id: "execute", label: "Execute" },
  ];

  // Human messages for the backend's 4xx error codes.
  const codeMessages: Record<string, string> = {
    revision_conflict: "This project's roles changed since you loaded them — reloaded to the latest. Review and try again.",
    unknown_role: "This role is not recognized.",
    invalid_style: "That style is not valid.",
    invalid_mode: "That mode is not valid.",
    unowned_capability: "That capability cannot be set for this role.",
  };

  // Server-persisted state (the baseline the draft diffs against).
  let roles: RolesConfiguration | null = $state(null);
  // Optimistic-locking token shared by every role; mutations send it as
  // base_revision and the server bumps it on success (409s if it moved).
  let revision = $state(0);
  // owned[role] -> the capability keys that role may render.
  let ownedByRole: Record<string, string[]> = $state({});

  // Per-role editable draft. Cloned from `roles` on load and after each
  // successful save/reset so "dirty" is a structural diff against the server.
  let drafts: Record<string, RoleConfig> = $state({});

  let loading = $state(true);
  let error: string | null = $state(null);
  // Per-role transient banners.
  let conflictNotice: string | null = $state(null);
  let busyRole: string | null = $state(null);
  let roleError: Record<string, string> = $state({});

  function cloneConfig(c: RoleConfig): RoleConfig {
    return { enabled: c.enabled, style: c.style, mode: c.mode, capabilities: { ...c.capabilities } };
  }

  function resetDraftsFrom(cfg: RolesConfiguration) {
    const next: Record<string, RoleConfig> = {};
    for (const r of ROLE_ORDER) next[r] = cloneConfig(cfg[r]);
    drafts = next;
  }

  async function reload() {
    const res = await getRoles(projectId);
    roles = res.roles;
    revision = res.revision;
    const owned: Record<string, string[]> = {};
    for (const o of res.owned) owned[o.role] = o.capabilities;
    ownedByRole = owned;
    resetDraftsFrom(res.roles);
  }

  async function load() {
    loading = true;
    error = null;
    try {
      await reload();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  // A role card is dirty when its draft differs from the server baseline.
  function isDirty(role: RoleName): boolean {
    if (!roles) return false;
    const base = roles[role];
    const draft = drafts[role];
    if (!draft) return false;
    if (draft.enabled !== base.enabled || draft.style !== base.style || draft.mode !== base.mode) return true;
    for (const key of ownedByRole[role] ?? []) {
      if ((draft.capabilities[key] ?? false) !== (base.capabilities[key] ?? false)) return true;
    }
    return false;
  }

  // Turn a capability key into a human-readable label:
  // `cross_repository_impact` -> "Cross repository impact".
  function humanize(key: string): string {
    const spaced = key.replace(/_/g, " ");
    return spaced.charAt(0).toUpperCase() + spaced.slice(1);
  }

  // Effective-behavior preview (plan §14), derived entirely client-side from
  // mode + capability toggles. `mode` sets the ceiling on how far the role
  // may act on its own; enabled capabilities become "can" lines, the role's
  // owned-but-disabled capabilities become "cannot" lines.
  function modeSummary(mode: string): string {
    switch (mode) {
      case "assist":
        return "advises only — proposes nothing on its own and makes no changes.";
      case "propose":
        return "proposes changes for approval but does not apply them.";
      case "execute":
        return "applies accepted changes directly.";
      default:
        return "";
    }
  }

  function effectivePreview(role: RoleName): { can: string[]; cannot: string[]; mode: string } {
    const draft = drafts[role];
    const owned = ownedByRole[role] ?? [];
    const can: string[] = [];
    const cannot: string[] = [];
    for (const key of owned) {
      if (draft?.capabilities[key]) can.push(humanize(key).toLowerCase());
      else cannot.push(humanize(key).toLowerCase());
    }
    return { can, cannot, mode: draft?.mode ?? "assist" };
  }

  function setStyle(role: RoleName, style: string) {
    clearRoleTransient();
    drafts[role].style = style;
  }
  function setMode(role: RoleName, mode: string) {
    clearRoleTransient();
    drafts[role].mode = mode;
  }
  function toggleEnabled(role: RoleName, value: boolean) {
    clearRoleTransient();
    drafts[role].enabled = value;
  }
  function toggleCapability(role: RoleName, key: string, value: boolean) {
    clearRoleTransient();
    drafts[role].capabilities[key] = value;
  }

  function clearRoleTransient() {
    conflictNotice = null;
  }

  async function save(role: RoleName) {
    if (!isDirty(role)) return;
    busyRole = role;
    conflictNotice = null;
    roleError = { ...roleError, [role]: "" };
    const draft = drafts[role];
    // Only send the capability keys this role owns.
    const capabilities: Record<string, boolean> = {};
    for (const key of ownedByRole[role] ?? []) capabilities[key] = draft.capabilities[key] ?? false;
    try {
      const res = await updateRole(
        projectId,
        role,
        { enabled: draft.enabled, style: draft.style, mode: draft.mode, capabilities },
        revision,
      );
      roles = res.roles;
      revision = res.revision;
      resetDraftsFrom(res.roles);
    } catch (e) {
      await handleMutationError(e, role);
    } finally {
      busyRole = null;
    }
  }

  async function reset(role: RoleName) {
    busyRole = role;
    conflictNotice = null;
    roleError = { ...roleError, [role]: "" };
    try {
      const res = await resetRole(projectId, role, revision);
      roles = res.roles;
      revision = res.revision;
      resetDraftsFrom(res.roles);
    } catch (e) {
      await handleMutationError(e, role);
    } finally {
      busyRole = null;
    }
  }

  async function handleMutationError(e: unknown, role: RoleName) {
    if (e instanceof ApiError && (e.status === 409 || e.code === "revision_conflict")) {
      conflictNotice = codeMessages.revision_conflict;
      try {
        await reload();
      } catch {
        /* surfaced on next interaction */
      }
    } else if (e instanceof ApiError) {
      roleError = { ...roleError, [role]: codeMessages[e.code ?? ""] ?? e.message };
    } else {
      roleError = { ...roleError, [role]: e instanceof Error ? e.message : String(e) };
    }
  }
</script>

<section aria-label="Project roles">
  {#if conflictNotice}
    <p class="conflict" role="alert" data-testid="conflict-notice">{conflictNotice}</p>
  {/if}

  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <p class="error" role="alert">Failed to load roles: {error}</p>
  {:else if roles}
    <div class="roles">
      {#each ROLE_ORDER as role (role)}
        {@const meta = ROLE_META[role]}
        {@const draft = drafts[role]}
        {@const owned = ownedByRole[role] ?? []}
        {@const preview = effectivePreview(role)}
        {@const dirty = isDirty(role)}
        <article class="card" aria-label={`${meta.label} role`}>
          <header class="card-head">
            <h3>{meta.label}</h3>
            <label class="switch">
              <input
                type="checkbox"
                checked={draft.enabled}
                onchange={(e) => toggleEnabled(role, e.currentTarget.checked)}
                aria-label={`Enable ${meta.label}`}
              />
              <span>Enabled</span>
            </label>
          </header>
          <p class="responsibility">{meta.responsibility}</p>

          <div class="field">
            <span class="field-label" id={`style-label-${role}`}>Style</span>
            <div class="segmented" role="radiogroup" aria-labelledby={`style-label-${role}`}>
              {#each STYLES as s (s.id)}
                <button
                  type="button"
                  role="radio"
                  aria-checked={draft.style === s.id}
                  class="segment"
                  class:active={draft.style === s.id}
                  onclick={() => setStyle(role, s.id)}
                >
                  {s.label}
                </button>
              {/each}
            </div>
          </div>

          <div class="field">
            <span class="field-label" id={`mode-label-${role}`}>Mode</span>
            <div class="segmented" role="radiogroup" aria-labelledby={`mode-label-${role}`}>
              {#each MODES as m (m.id)}
                <button
                  type="button"
                  role="radio"
                  aria-checked={draft.mode === m.id}
                  class="segment"
                  class:active={draft.mode === m.id}
                  onclick={() => setMode(role, m.id)}
                >
                  {m.label}
                </button>
              {/each}
            </div>
          </div>

          {#if owned.length > 0}
            <div class="field">
              <span class="field-label">Capabilities</span>
              <ul class="capabilities">
                {#each owned as key (key)}
                  <li>
                    <label class="switch">
                      <input
                        type="checkbox"
                        checked={draft.capabilities[key] ?? false}
                        onchange={(e) => toggleCapability(role, key, e.currentTarget.checked)}
                        aria-label={humanize(key)}
                      />
                      <span>{humanize(key)}</span>
                    </label>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}

          <div class="preview" aria-label={`${meta.label} effective behavior`}>
            <p class="preview-head">
              Effective behavior — <strong>{meta.label}</strong> {modeSummary(preview.mode)}
            </p>
            {#if preview.can.length > 0}
              <p class="can"><span class="tag can">can</span> {preview.can.join(", ")}</p>
            {/if}
            {#if preview.cannot.length > 0}
              <p class="cannot"><span class="tag cannot">cannot</span> {preview.cannot.join(", ")}</p>
            {/if}
          </div>

          {#if roleError[role]}
            <p class="error" role="alert" data-testid={`role-error-${role}`}>{roleError[role]}</p>
          {/if}

          <div class="card-actions">
            <button type="button" class="btn" onclick={() => reset(role)} disabled={busyRole === role}>
              Reset defaults
            </button>
            <button
              type="button"
              class="btn primary"
              onclick={() => save(role)}
              disabled={!dirty || busyRole === role}
            >
              {busyRole === role ? "Saving…" : "Save"}
            </button>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</section>

<style>
  .conflict {
    background: color-mix(in srgb, var(--color-warning) 14%, var(--color-surface));
    color: var(--color-warning);
    border: 1px solid color-mix(in srgb, var(--color-warning) 30%, transparent);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .roles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 1rem;
  }
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.05rem 1.15rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    box-sizing: border-box;
    min-width: 0;
  }
  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }
  h3 {
    margin: 0;
    font-size: 1.05rem;
  }
  .responsibility {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .field {
    display: grid;
    gap: 0.35rem;
  }
  .field-label {
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .segmented {
    display: inline-flex;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
    align-self: flex-start;
  }
  .segment {
    font: inherit;
    font-size: 0.8rem;
    font-weight: 600;
    padding: 0.35rem 0.7rem;
    min-height: 36px;
    border: 0;
    border-right: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
  }
  .segment:last-child {
    border-right: 0;
  }
  .segment.active {
    background: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .segment:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  .capabilities {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.35rem;
  }
  .switch {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.85rem;
    color: var(--color-text);
    cursor: pointer;
  }
  .switch input {
    accent-color: var(--color-accent);
    width: 16px;
    height: 16px;
  }
  .preview {
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.7rem;
    display: grid;
    gap: 0.3rem;
  }
  .preview p {
    margin: 0;
    font-size: 0.82rem;
  }
  .preview-head {
    color: var(--color-text-muted);
  }
  .tag {
    display: inline-block;
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
    margin-right: 0.35rem;
  }
  .tag.can {
    color: var(--color-success);
    background: color-mix(in srgb, var(--color-success) 16%, transparent);
  }
  .tag.cannot {
    color: var(--color-text-muted);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
  }
  .card-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: auto;
  }
  .btn {
    font: inherit;
    font-weight: 600;
    font-size: 0.82rem;
    padding: 0.4rem 0.8rem;
    min-height: 40px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-strong);
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    border-color: var(--color-accent);
  }
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .btn.primary {
    background: var(--color-accent);
    border-color: var(--color-accent);
    color: var(--color-accent-contrast);
  }
</style>
