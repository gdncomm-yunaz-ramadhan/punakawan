<script lang="ts">
  import BottomSheet from "../../src/lib/components/overlay/BottomSheet.svelte";

  let open = $state(false);
</script>

<button type="button">Before</button>
<button type="button" onclick={() => (open = true)}>Open</button>
<BottomSheet {open} title="Example sheet" onclose={() => (open = false)}>
  <button type="button">First</button>
  <button type="button">Second</button>
</BottomSheet>
<button type="button">After</button>
