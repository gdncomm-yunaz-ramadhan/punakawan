<script lang="ts">
  import Dialog from "../../src/lib/components/overlay/Dialog.svelte";

  let open = $state(false);
</script>

<button type="button" onclick={() => (open = true)}>Open</button>
<Dialog {open} title="Example dialog" onclose={() => (open = false)}>
  <p>Dialog body</p>
</Dialog>
