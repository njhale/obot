<script lang="ts">
	import { type Project } from '$lib/services';
	import { hasTool } from '$lib/tools';
	import { getProjectTools } from '$lib/context/projectTools.svelte';
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import { Save } from 'lucide-svelte/icons';
	import { showDialog } from '$lib/components/MemoriesDialog.svelte';

	interface Props {
		project: Project;
		memoryContent?: string;
	}

	let { project, memoryContent = '' }: Props = $props();
	const projectTools = getProjectTools();

	function openMemoriesDialog() {
		showDialog(project);
	}
</script>

{#if hasTool(projectTools.tools, 'memory')}
	<button
		class="text-gray flex cursor-pointer items-center gap-1 text-xs underline"
		onclick={openMemoriesDialog}
		use:tooltip={'Open memories'}
	>
		<Save class="h-3 w-3" />
		Memories
	</button>
{/if}
