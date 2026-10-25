<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	type Props = Omit<HTMLInputAttributes, 'value'> & {
		value?: string;
		class?: string;
	};

	let { value = $bindable(''), class: className = '', ...rest }: Props = $props();
</script>

<input bind:value class={`text-input ${className}`.trim()} {...rest} />

<style>
	.text-input {
		width: 100%;
		min-width: 0;
		border: 0;
		background: transparent;
		box-shadow: none;
		padding: 0.9rem 0.85rem;
		color: var(--pixel-text);
	}

	.text-input::placeholder {
		color: var(--pixel-muted);
		opacity: 1;
	}
</style>
