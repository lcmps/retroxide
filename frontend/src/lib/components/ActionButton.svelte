<script lang="ts">
	import { Button } from 'bits-ui';
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';

	type Props = Omit<HTMLButtonAttributes, 'children'> & {
		children?: Snippet;
		variant?: 'primary' | 'secondary' | 'danger';
	};

	let {
		children,
		class: className = '',
		variant = 'primary',
		type = 'button',
		disabled = false,
		...rest
	}: Props = $props();
</script>

<Button.Root
	{type}
	{disabled}
	class={`action-button action-button--${variant} ${className}`.trim()}
	{...rest}
>
	{#if children}
		{@render children()}
	{/if}
</Button.Root>

<style>
	:global(.action-button) {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 9rem;
		min-height: 2rem;
		padding: 0.7rem 1rem;
		border: 0;
		border-radius: 0.4rem;
		font-size: 0.55rem;
		font-weight: 400;
		text-transform: uppercase;
		cursor: pointer;
		box-shadow: inset -2px -2px 0 rgba(0, 0, 0, 0.18);
		transition: opacity 120ms ease, border-color 120ms ease;
	}

	:global(.action-button--primary) {
		background: var(--pixel-accent);
		color: #120d07;
	}

	:global(.action-button--secondary) {
		background: transparent;
		color: inherit;
	}

	:global(.action-button--danger) {
		background: var(--pixel-danger);
		border-color: #e18a78;
		color: #fff3ef;
	}

	:global(.action-button:disabled) {
		opacity: 0.6;
		cursor: not-allowed;
	}
</style>
