<script lang="ts">
  import Dialog from "../../src/lib/components/overlay/Dialog.svelte";

  let open = $state(false);
</script>

<button type="button">Before</button>
<button type="button" onclick={() => (open = true)}>Open</button>
<Dialog {open} title="Example dialog" onclose={() => (open = false)}>
  <button type="button">First</button>
  <button type="button">Second</button>
</Dialog>
<button type="button">After</button>
