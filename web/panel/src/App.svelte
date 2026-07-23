<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, type SystemInfo } from "./lib/api/client";
  import { getPath } from "./lib/router/router.svelte";
  import Sidebar from "./lib/components/Sidebar.svelte";
  import TopBar from "./lib/components/TopBar.svelte";
  import Overview from "./routes/overview/Overview.svelte";
  import WorkspacesList from "./routes/workspaces/WorkspacesList.svelte";
  import WorkspaceSummary from "./routes/workspaces/WorkspaceSummary.svelte";
  import SessionsList from "./routes/sessions/SessionsList.svelte";
  import SessionDetail from "./routes/sessions/SessionDetail.svelte";

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
</script>

<div class="shell">
  <Sidebar />
  <div class="content-area">
    <TopBar {system} />
    <main>
      {#if systemError}
        <p role="alert" class="error">Failed to reach the panel server: {systemError}</p>
      {/if}

      {#if getPath() === "/" || getPath() === ""}
        <Overview />
      {:else if getPath() === "/workspaces"}
        <WorkspacesList />
      {:else if sessionDetailPath.exec(getPath())}
        {@const match = sessionDetailPath.exec(getPath())}
        <SessionDetail
          workspaceId={decodeURIComponent(match?.[1] ?? "")}
          sessionId={decodeURIComponent(match?.[2] ?? "")}
        />
      {:else if sessionsListPath.exec(getPath())}
        {@const match = sessionsListPath.exec(getPath())}
        <SessionsList workspaceId={decodeURIComponent(match?.[1] ?? "")} />
      {:else if workspaceDetailPath.exec(getPath())}
        {@const match = workspaceDetailPath.exec(getPath())}
        <WorkspaceSummary workspaceId={decodeURIComponent(match?.[1] ?? "")} />
      {:else}
        <p>Not found.</p>
      {/if}
    </main>
  </div>
</div>

<style>
  :global(body) {
    margin: 0;
    font-family: system-ui, sans-serif;
    color: #1a1a1a;
  }
  .shell {
    display: flex;
    min-height: 100vh;
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
  }
  .error {
    color: #b00020;
  }

  @media (max-width: 720px) {
    .shell {
      flex-direction: column;
    }
  }
</style>
