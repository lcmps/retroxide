<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		label: string;
		children?: Snippet;
		class?: string;
	};

	let { label, children, class: className = '' }: Props = $props();
</script>

<label class={`form-field ${className}`.trim()}>
	<span class="form-field__label">{label}</span>
	{@render children?.()}
</label>

<style>
	.form-field {
		display: grid;
		gap: 0.75rem;
	}

	.form-field__label {
		font-size: 0.8rem;
		font-weight: 800;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--pixel-muted);
	}
</style>
