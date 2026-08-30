<script lang="ts">
  import { onMount } from "svelte";
  import {
    getRoles,
    updateRole,
    resetRole,
    ApiError,
    type RolePreference,
    type RolesConfiguration,
  } from "../../lib/api/client";
  import { roleAvatars } from "../../lib/assets/roles";
  import Card from "../../lib/components/cards/Card.svelte";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  // The four roles, in fixed presentation order.
  type RoleName = "semar" | "gareng" | "petruk" | "bagong";
  const ROLE_ORDER: RoleName[] = ["semar", "gareng", "petruk", "bagong"];

  const MAX_INSTRUCTIONS_LENGTH = 2000;

  // Display name, one-line responsibility, a collapsed communication summary,
  // and the role's short principle (plan §8-11; wayang-nuance refinement).
  const ROLE_META: Record<
    RoleName,
    { label: string; responsibility: string; communication: string; principle: string }
  > = {
    semar: {
      label: "Semar",
      responsibility:
        "Coordinates: interprets intent, selects workflows, manages the change dossier, creates handoff capsules.",
      communication: "Calm and purpose-oriented. Summarizes decisions without hiding disagreement.",
      principle: "Ground the work.",
    },
    gareng: {
      label: "Gareng",
      responsibility:
        "Challenges: finds missing context, detects contradictions, analyzes cross-repository impact and risk.",
      communication: "Careful and evidence-backed. Focuses on meaningful consequences.",
      principle: "Notice what others miss.",
    },
    petruk: {
      label: "Petruk",
      responsibility:
        "Plans and builds: proposes solutions, decomposes tasks, implements accepted work across repositories.",
      communication: "Practical and solution-oriented. Prefers the simplest sufficient approach.",
      principle: "Make the idea useful.",
    },
    bagong: {
      label: "Bagong",
      responsibility: "Verifies: independently checks plan conformance, tests, coverage, and dossier claims.",
      communication: "Direct and plain. Separates what was proven from what was merely claimed.",
      principle: "Say what is true.",
    },
  };

  const STYLES = [
    { id: "strict", label: "Strict" },
    { id: "balanced", label: "Balanced" },
    { id: "creative", label: "Creative" },
  ];

  // Human messages for the backend's 4xx error codes.
  const codeMessages: Record<string, string> = {
    revision_conflict: "This project's roles changed since you loaded them — reloaded to the latest. Review and try again.",
    unknown_role: "This role is not recognized.",
    invalid_style: "That style is not valid.",
    instructions_too_long: `Instructions must be ${MAX_INSTRUCTIONS_LENGTH} characters or fewer.`,
  };

  // Server-persisted state (the baseline the draft diffs against).
  let roles: RolesConfiguration | null = $state(null);
  // Optimistic-locking token shared by every role; mutations send it as
  // base_revision and the server bumps it on success (409s if it moved).
  let revision = $state(0);

  // Per-role editable draft. Cloned from `roles` on load and after each
  // successful save/reset so "dirty" is a structural diff against the server.
  let drafts: Record<string, RolePreference> = $state({});

  let loading = $state(true);
  let error: string | null = $state(null);
  // Per-role transient banners.
  let conflictNotice: string | null = $state(null);
  let busyRole: string | null = $state(null);
  let roleError: Record<string, string> = $state({});

  function cloneConfig(c: RolePreference): RolePreference {
    return { style: c.style, instructions: c.instructions };
  }

  function resetDraftsFrom(cfg: RolesConfiguration) {
    const next: Record<string, RolePreference> = {};
    for (const r of ROLE_ORDER) next[r] = cloneConfig(cfg[r]);
    drafts = next;
  }

  async function reload() {
    const res = await getRoles(projectId);
    roles = res.roles;
    revision = res.revision;
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
    return draft.style !== base.style || draft.instructions !== base.instructions;
  }

  function setStyle(role: RoleName, style: string) {
    clearRoleTransient();
    drafts[role].style = style;
  }
  function setInstructions(role: RoleName, instructions: string) {
    clearRoleTransient();
    drafts[role].instructions = instructions;
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
    try {
      const res = await updateRole(projectId, role, { style: draft.style, instructions: draft.instructions }, revision);
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
        {@const dirty = isDirty(role)}
        <article class="role-cell" aria-label={`${meta.label} role`}>
        <Card>
          {#snippet header()}
            <div class="role-identity">
              <img
                class="role-portrait"
                src={roleAvatars[role]}
                alt={`${meta.label} portrait`}
                width="64"
                height="72"
                loading="lazy"
                decoding="async"
              />
              <h3>{meta.label}</h3>
            </div>
          {/snippet}

          <p class="responsibility">{meta.responsibility}</p>

          <details class="comm-summary">
            <summary>
              <span class="principle">{meta.principle}</span>
              <span class="comm-hint">How this role communicates</span>
            </summary>
            <p>{meta.communication}</p>
          </details>

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
            <label class="field-label" for={`instructions-${role}`}>Instructions</label>
            <textarea
              id={`instructions-${role}`}
              class="instructions"
              rows="3"
              maxlength={MAX_INSTRUCTIONS_LENGTH}
              placeholder="Optional free-text guidance appended to this role's prompt."
              value={draft.instructions}
              oninput={(e) => setInstructions(role, e.currentTarget.value)}
            ></textarea>
          </div>

          {#if roleError[role]}
            <p class="error" role="alert" data-testid={`role-error-${role}`}>{roleError[role]}</p>
          {/if}

          {#snippet footer()}
            <div class="card-actions">
              <Button variant="secondary" fullWidth onclick={() => reset(role)} disabled={busyRole === role}>
                Reset defaults
              </Button>
              <Button
                variant="primary"
                fullWidth
                onclick={() => save(role)}
                disabled={!dirty || busyRole === role}
              >
                {busyRole === role ? "Saving…" : "Save"}
              </Button>
            </div>
          {/snippet}
        </Card>
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
    /* Hard column cap: 1 column below 640px so nothing overflows on phones. */
    grid-template-columns: minmax(0, 1fr);
    gap: 1rem;
  }
  /* 2 columns on tablets. */
  @media (min-width: 640px) {
    .roles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  /* At most 4 columns on desktop — the four roles fit in a single row and
     never over-column on ultrawide displays. */
  @media (min-width: 1024px) {
    .roles {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }
  /* Each grid cell just carries the role's accessible label; the visual
     surface is the shared Setara Card inside it. */
  .role-cell {
    min-width: 0;
    display: flex;
  }
  .role-cell :global(.card) {
    width: 100%;
    gap: 0.75rem;
  }
  .role-identity {
    display: flex;
    align-items: center;
    gap: 0.7rem;
    min-width: 0;
  }
  .role-portrait {
    flex: none;
    width: 64px;
    height: 72px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border);
    /* contain (not cover) so the full wayang figure shows, uncropped. */
    object-fit: contain;
    background: var(--color-surface-subtle);
    padding: 2px;
  }
  h3 {
    margin: 0;
    font-size: 1.1rem;
  }
  .responsibility {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .comm-summary {
    font-size: 0.8rem;
  }
  .comm-summary > summary {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    cursor: pointer;
    list-style: none;
  }
  .comm-summary > summary::-webkit-details-marker {
    display: none;
  }
  .comm-summary .principle {
    font-weight: 600;
    color: var(--color-text);
  }
  .comm-summary .comm-hint {
    color: var(--color-text-muted);
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .comm-summary > p {
    margin: 0.35rem 0 0;
    color: var(--color-text-muted);
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
  /* Full-width segmented control with equal-width segments. */
  .segmented {
    display: flex;
    width: 100%;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .segment {
    font: inherit;
    font-size: 0.8rem;
    font-weight: 600;
    flex: 1 1 0;
    padding: 0.4rem 0.5rem;
    min-height: 38px;
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
  .instructions {
    font: inherit;
    font-size: 0.85rem;
    padding: 0.5rem 0.6rem;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    resize: vertical;
    min-height: 4.5rem;
  }
  .instructions:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  /* Proportional action row: Reset and Save share the width equally. */
  .card-actions {
    display: flex;
    gap: 0.5rem;
  }
</style>
