<script lang="ts">
	import { type Project } from '$lib/services';
	import { showDialog } from '$lib/components/MemoriesDialog.svelte';

	interface Props {
		project: Project;
	}

	let { project }: Props = $props();

	export async function show() {
		showDialog(project);
	}
</script>
