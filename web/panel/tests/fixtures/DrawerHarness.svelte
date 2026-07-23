<script lang="ts">
  import Drawer from "../../src/lib/components/overlay/Drawer.svelte";

  let open = $state(false);
</script>

<button type="button" onclick={() => (open = true)}>Open</button>
<Drawer {open} title="Example drawer" onclose={() => (open = false)}>
  <p>Drawer body</p>
</Drawer>
