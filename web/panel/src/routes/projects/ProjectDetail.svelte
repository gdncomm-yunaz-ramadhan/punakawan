<script lang="ts">
  import { onMount } from "svelte";
  import { getProject, type ProjectDetail, type Availability } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import Tabs from "../../lib/components/Tabs.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard, { type MetricAccent } from "../../lib/components/cards/MetricCard.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ProjectMetadata from "./ProjectMetadata.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let project: ProjectDetail | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  // Summary + Metadata are built (Phase 2). The rest are declared so the
  // tab bar reflects the project's full information architecture, but they
  // render an explicit "coming soon" panel - never faked data - because
  // those phases have not shipped yet.
  const activeTabs = new Set(["summary", "metadata"]);
  const tabs = [
    { id: "summary", label: "Summary" },
    { id: "metadata", label: "Metadata" },
    { id: "workflows", label: "Workflows" },
    { id: "knowledge", label: "Knowledge" },
    { id: "tasks", label: "Tasks" },
    { id: "plans", label: "Plans" },
    { id: "sessions", label: "Sessions" },
    { id: "health", label: "Health" },
  ];
  let activeId = $state("summary");

  async function load(id: string) {
    loading = true;
    error = null;
    project = null;
    try {
      project = await getProject(id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load(projectId);
    return onPanelEvent(() => load(projectId));
  });
  $effect(() => {
    load(projectId);
  });

  interface Metric {
    label: string;
    value: number;
    accent: MetricAccent;
    icon: string;
  }
  function buildMetrics(p: ProjectDetail | null): Metric[] {
    if (!p) return [];
    return [
      { label: "Repositories", value: p.repository_count, accent: "indigo", icon: "▤" },
      { label: "Open tasks", value: p.open_task_count, accent: "gold", icon: "◔" },
      { label: "Blocked tasks", value: p.blocked_task_count, accent: "danger", icon: "▰" },
      { label: "Active sessions", value: p.active_session_count, accent: "teal", icon: "◈" },
      { label: "Knowledge records", value: p.knowledge_count, accent: "terracotta", icon: "❋" },
      { label: "Metadata entries", value: p.metadata_count, accent: "success", icon: "≣" },
    ];
  }
  const metrics = $derived(buildMetrics(project));
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load this project: {error}</p>
{:else if project}
  <nav class="breadcrumb" aria-label="Breadcrumb">
    <a
      href="/projects"
      onclick={(e) => {
        e.preventDefault();
        navigate("/projects");
      }}>Projects</a
    >
    <span aria-hidden="true">/</span>
    <span aria-current="page">{project.name || project.id}</span>
  </nav>

  <header class="head">
    <div class="title">
      <h1>{project.name || project.id}</h1>
      {#if project.primary}<span class="tag primary">primary</span>{/if}
      {#if project.pinned}<span title="Pinned" aria-label="Pinned">📌</span>{/if}
    </div>
    <StatusBadge availability={project.availability as Availability} />
  </header>
  {#if project.description}<p class="description">{project.description}</p>{/if}
  <p class="path">{project.path}</p>

  <Tabs {tabs} {activeId} onchange={(id) => (activeId = id)} ariaLabel="Project sections" />

  {#if activeId === "summary"}
    <div id="tabpanel-summary" role="tabpanel" aria-labelledby="tab-summary">
      <BentoGrid>
        {#each metrics as m (m.label)}
          <MetricCard size="small" label={m.label} value={m.value} accent={m.accent} icon={m.icon} />
        {/each}
      </BentoGrid>
    </div>
  {:else if activeId === "metadata"}
    <div id="tabpanel-metadata" role="tabpanel" aria-labelledby="tab-metadata">
      <ProjectMetadata {projectId} />
    </div>
  {:else}
    {@const tab = tabs.find((t) => t.id === activeId)}
    <div id={`tabpanel-${activeId}`} role="tabpanel" aria-labelledby={`tab-${activeId}`}>
      <EmptyStateCard
        title={`${tab?.label ?? "This section"} — coming soon`}
        message="This section arrives in a later phase. There is no data to show yet."
      />
    </div>
  {/if}
{/if}

<style>
  .error {
    color: var(--color-danger);
  }
  .breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.82rem;
    color: var(--color-text-muted);
    margin-bottom: 0.5rem;
  }
  .breadcrumb a {
    color: var(--color-accent);
    text-decoration: none;
  }
  .breadcrumb a:hover {
    text-decoration: underline;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .title {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  h1 {
    font-size: 1.3rem;
    margin: 0;
  }
  .tag.primary {
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .description {
    margin: 0.4rem 0 0;
    color: var(--color-text);
    font-size: 0.92rem;
  }
  .path {
    color: var(--color-text-muted);
    font-size: 0.82rem;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    margin: 0.15rem 0 1rem;
  }
</style>
