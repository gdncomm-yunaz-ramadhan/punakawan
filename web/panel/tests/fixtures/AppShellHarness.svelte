<script lang="ts">
  import AppShell from "../../src/lib/components/AppShell.svelte";
  import type { SystemInfo } from "../../src/lib/api/client";

  interface Props {
    system: SystemInfo | null;
  }
  let { system }: Props = $props();
</script>

<AppShell {system}>
  <p data-testid="shell-content">Page content</p>
</AppShell>
