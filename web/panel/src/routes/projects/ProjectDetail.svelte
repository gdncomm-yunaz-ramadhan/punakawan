<script lang="ts">
  import { getProject, type ProjectDetail, type Availability } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import Tabs from "../../lib/components/Tabs.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard, { type MetricAccent } from "../../lib/components/cards/MetricCard.svelte";
  import type { IconName } from "../../lib/components/Icon.svelte";
  import ProjectMetadata from "./ProjectMetadata.svelte";
  import ProjectRoles from "./ProjectRoles.svelte";
  import ProjectWorkflows from "./ProjectWorkflows.svelte";
  import ProjectPlans from "./ProjectPlans.svelte";
  import ProjectHealth from "./ProjectHealth.svelte";
  import ProjectKnowledge from "./ProjectKnowledge.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let project: ProjectDetail | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  // Every tab renders real project-scoped data. The tab bar mirrors the
  // project's full information architecture; each panel reads from the
  // matching /projects/{id}/... endpoint with its own empty/error states.
  const activeTabs = new Set([
    "summary",
    "workflows",
    "knowledge",
    "plans",
    "metadata",
    "settings",
  ]);
  const tabs = [
    { id: "summary", label: "Summary", icon: "dashboard" as IconName },
    { id: "plans", label: "Plans", icon: "file" as IconName },
    { id: "workflows", label: "Workflows", icon: "git-branch" as IconName },
    { id: "knowledge", label: "Knowledge", icon: "book" as IconName },
    { id: "metadata", label: "Metadata", icon: "database" as IconName },
    { id: "settings", label: "Settings", icon: "settings" as IconName },
  ];

  function tabFromUrl(): string {
    if (typeof window === "undefined") return "summary";
    const requested = new URL(window.location.href).searchParams.get("tab") ?? "summary";
    return activeTabs.has(requested) ? requested : "summary";
  }

  let activeId = $state(tabFromUrl());

  function selectTab(id: string) {
    activeId = id;
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (id === "summary") url.searchParams.delete("tab");
    else url.searchParams.set("tab", id);
    window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  }

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

  // The effect is the single trigger for both the first load and any later
  // projectId change - effects also run after the first render, so loading
  // from onMount as well would fire the very same request twice per open.
  $effect(() => {
    load(projectId);
  });

  interface Metric {
    label: string;
    value: number;
    accent: MetricAccent;
    icon: IconName;
  }
  function buildMetrics(p: ProjectDetail | null): Metric[] {
    if (!p) return [];
    return [
      { label: "Repositories", value: p.repository_count, accent: "indigo", icon: "folder" },
      { label: "Knowledge records", value: p.knowledge_count, accent: "terracotta", icon: "book" },
      { label: "Metadata entries", value: p.metadata_count, accent: "success", icon: "database" },
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

  <Tabs {tabs} {activeId} onchange={selectTab} ariaLabel="Project sections" />

  {#if activeId === "summary"}
    <div id="tabpanel-summary" role="tabpanel" aria-labelledby="tab-summary">
      <BentoGrid>
        {#each metrics as m (m.label)}
          <MetricCard size="small" columns={3} label={m.label} value={m.value} accent={m.accent} icon={m.icon} />
        {/each}
      </BentoGrid>
    </div>
  {:else if activeId === "workflows"}
    <div id="tabpanel-workflows" role="tabpanel" aria-labelledby="tab-workflows">
      <ProjectWorkflows {projectId} />
    </div>
  {:else if activeId === "knowledge"}
    <div id="tabpanel-knowledge" role="tabpanel" aria-labelledby="tab-knowledge">
      <ProjectKnowledge {projectId} />
    </div>
  {:else if activeId === "plans"}
    <div id="tabpanel-plans" role="tabpanel" aria-labelledby="tab-plans">
      <ProjectPlans {projectId} />
    </div>
  {:else if activeId === "metadata"}
    <div id="tabpanel-metadata" role="tabpanel" aria-labelledby="tab-metadata">
      <ProjectMetadata {projectId} />
    </div>
  {:else if activeId === "settings"}
    <div id="tabpanel-settings" role="tabpanel" aria-labelledby="tab-settings" class="settings-sections">
      <h2>Role prompt preferences</h2>
      <ProjectRoles {projectId} />
      <h2>Diagnostics</h2>
      <ProjectHealth {projectId} />
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
    /* Long paths wrap instead of forcing horizontal page scroll. */
    overflow-wrap: anywhere;
  }
</style>
