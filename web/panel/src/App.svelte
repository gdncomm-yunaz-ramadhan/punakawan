<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, type SystemInfo } from "./lib/api/client";
  import { getPath } from "./lib/router/router.svelte";
  import AppShell from "./lib/components/AppShell.svelte";
  import Overview from "./routes/overview/Overview.svelte";
  import WorkspacesList from "./routes/workspaces/WorkspacesList.svelte";
  import WorkspaceSummary from "./routes/workspaces/WorkspaceSummary.svelte";
  import SessionsList from "./routes/sessions/SessionsList.svelte";
  import SessionDetail from "./routes/sessions/SessionDetail.svelte";
  import TasksPage from "./routes/tasks/TasksPage.svelte";
  import KnowledgeList from "./routes/knowledge/KnowledgeList.svelte";
  import KnowledgeDetail from "./routes/knowledge/KnowledgeDetail.svelte";
  import GlobalSearch from "./routes/search/GlobalSearch.svelte";
  import ApprovalsList from "./routes/approvals/ApprovalsList.svelte";
  import SystemPage from "./routes/system/SystemPage.svelte";
  import Showcase from "./routes/showcase/Showcase.svelte";
  import StartReview from "./routes/review/StartReview.svelte";
  import ReviewMode from "./routes/review/ReviewMode.svelte";

  let system: SystemInfo | null = $state(null);
  let systemError: string | null = $state(null);

  onMount(async () => {
    try {
      system = await getSystem();
    } catch (e) {
      systemError = e instanceof Error ? e.message : String(e);
    }
  });

  const workspaceDetailPath = /^\/workspaces\/([^/]+)$/;
  const sessionsListPath = /^\/workspaces\/([^/]+)\/sessions$/;
  const sessionDetailPath = /^\/workspaces\/([^/]+)\/sessions\/([^/]+)$/;
  const tasksPath = /^\/workspaces\/([^/]+)\/tasks$/;
  const knowledgeListPath = /^\/workspaces\/([^/]+)\/knowledge$/;
  const knowledgeDetailPath = /^\/workspaces\/([^/]+)\/knowledge\/([^/]+)$/;
  const approvalsPath = /^\/workspaces\/([^/]+)\/approvals$/;
  const reviewModePath = /^\/reviews\/([^/]+)$/;
  const startReviewPath = /^\/artifacts\/(plan|retrieval_recipe)\/review\/new$/;
</script>

<AppShell {system}>
  {#if systemError}
    <p role="alert" class="error">Failed to reach the panel server: {systemError}</p>
  {/if}

  {#if getPath() === "/" || getPath() === ""}
    <Overview />
  {:else if getPath() === "/workspaces"}
    <WorkspacesList />
  {:else if getPath() === "/search"}
    <GlobalSearch />
  {:else if getPath() === "/system"}
    <SystemPage />
  {:else if getPath() === "/showcase"}
    <Showcase />
  {:else if startReviewPath.exec(getPath())}
    {@const match = startReviewPath.exec(getPath())}
    <StartReview artifactType={(match?.[1] as "plan" | "retrieval_recipe") ?? "plan"} />
  {:else if reviewModePath.exec(getPath())}
    {@const match = reviewModePath.exec(getPath())}
    <ReviewMode reviewId={decodeURIComponent(match?.[1] ?? "")} />
  {:else if approvalsPath.exec(getPath())}
    {@const match = approvalsPath.exec(getPath())}
    <ApprovalsList workspaceId={decodeURIComponent(match?.[1] ?? "")} />
  {:else if knowledgeDetailPath.exec(getPath())}
    {@const match = knowledgeDetailPath.exec(getPath())}
    <KnowledgeDetail
      workspaceId={decodeURIComponent(match?.[1] ?? "")}
      knowledgeId={decodeURIComponent(match?.[2] ?? "")}
    />
  {:else if knowledgeListPath.exec(getPath())}
    {@const match = knowledgeListPath.exec(getPath())}
    <KnowledgeList workspaceId={decodeURIComponent(match?.[1] ?? "")} />
  {:else if sessionDetailPath.exec(getPath())}
    {@const match = sessionDetailPath.exec(getPath())}
    <SessionDetail
      workspaceId={decodeURIComponent(match?.[1] ?? "")}
      sessionId={decodeURIComponent(match?.[2] ?? "")}
    />
  {:else if sessionsListPath.exec(getPath())}
    {@const match = sessionsListPath.exec(getPath())}
    <SessionsList workspaceId={decodeURIComponent(match?.[1] ?? "")} />
  {:else if tasksPath.exec(getPath())}
    {@const match = tasksPath.exec(getPath())}
    <TasksPage workspaceId={decodeURIComponent(match?.[1] ?? "")} />
  {:else if workspaceDetailPath.exec(getPath())}
    {@const match = workspaceDetailPath.exec(getPath())}
    <WorkspaceSummary workspaceId={decodeURIComponent(match?.[1] ?? "")} />
  {:else}
    <p>Not found.</p>
  {/if}
</AppShell>

<style>
  :global(body) {
    margin: 0;
    font-family: system-ui, sans-serif;
  }
  .error {
    color: var(--color-danger);
  }
</style>
