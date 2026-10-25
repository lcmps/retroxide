<script lang="ts">
	import { Button } from 'bits-ui';
	import type { HTMLButtonAttributes } from 'svelte/elements';

	type Props = Omit<HTMLButtonAttributes, 'children'> & {
		icon: string;
		label: string;
		activeIcon?: string;
		active?: boolean;
	};

	let {
		icon,
		label,
		activeIcon,
		active = false,
		class: className = '',
		type = 'button',
		disabled = false,
		...rest
	}: Props = $props();
</script>

<Button.Root
	{type}
	{disabled}
	aria-label={label}
	title={label}
	class={`icon-button ${className}`.trim()}
	{...rest}
>
	<img src={active && activeIcon ? activeIcon : icon} alt="" aria-hidden="true" />
</Button.Root>

<style>
	:global(.icon-button) {
		display: inline-grid;
		place-items: center;
		width: 2.5rem;
		height: 2.5rem;
		min-width: 2.5rem;
		min-height: 2.5rem;
		font-size: 0;
		padding: 0;
		border: 0;
		background: transparent;
		cursor: pointer;
	}

	:global(.icon-button img) {
		display: block;
		width: 100%;
		height: 100%;
		object-fit: contain;
		image-rendering: pixelated;
	}

	:global(.icon-button:disabled) {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
