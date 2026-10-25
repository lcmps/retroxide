<script lang="ts">
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import type { Snippet } from 'svelte';

	type Props = Omit<HTMLButtonAttributes, 'children'> & {
		children?: Snippet;
		ariaLabel?: string;
	};

	let {
		children,
		class: className = '',
		type = 'button',
		disabled = false,
		ariaLabel,
		...rest
	}: Props = $props();
</script>

<button
	{type}
	{disabled}
	aria-label={ariaLabel}
	class={`pixel-button ${className}`.trim()}
	{...rest}
>
	{#if children}
		{@render children()}
	{/if}
</button>
