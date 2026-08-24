<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, type SystemInfo } from "./lib/api/client";
  import { getPath } from "./lib/router/router.svelte";
  import AppShell from "./lib/components/AppShell.svelte";
  import ProjectsList from "./routes/projects/ProjectsList.svelte";
  import ProjectDetail from "./routes/projects/ProjectDetail.svelte";
  import SystemPage from "./routes/system/SystemPage.svelte";
  import DeliveriesList from "./routes/deliveries/DeliveriesList.svelte";
  import DeliveryDetail from "./routes/deliveries/DeliveryDetail.svelte";

  let system: SystemInfo | null = $state(null);
  let systemError: string | null = $state(null);

  onMount(async () => {
    try {
      system = await getSystem();
    } catch (e) {
      systemError = e instanceof Error ? e.message : String(e);
    }
  });

  const projectDetailPath = /^\/projects\/([^/]+)$/;
  const deliveryDetailPath = /^\/deliveries\/([^/]+)$/;
</script>

<AppShell {system}>
  {#if systemError}
    <p role="alert" class="error">Failed to reach the panel server: {systemError}</p>
  {/if}

  {#if getPath() === "/" || getPath() === ""}
    <ProjectsList />
  {:else if getPath() === "/projects"}
    <ProjectsList />
  {:else if getPath() === "/settings"}
    <SystemPage />
  {:else if getPath() === "/deliveries"}
    <DeliveriesList />
  {:else if deliveryDetailPath.exec(getPath())}
    {@const match = deliveryDetailPath.exec(getPath())}
    <DeliveryDetail orchestrationId={decodeURIComponent(match?.[1] ?? "")} />
  {:else if projectDetailPath.exec(getPath())}
    {@const match = projectDetailPath.exec(getPath())}
    <ProjectDetail projectId={decodeURIComponent(match?.[1] ?? "")} />
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
