<script lang="ts">
  import BottomSheet from "../../src/lib/components/overlay/BottomSheet.svelte";

  let open = $state(false);
</script>

<button type="button" onclick={() => (open = true)}>Open</button>
<BottomSheet {open} title="Example sheet" onclose={() => (open = false)}>
  <p>Sheet body</p>
</BottomSheet>
