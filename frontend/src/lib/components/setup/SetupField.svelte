<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	type Props = Omit<HTMLInputAttributes, 'value'> & {
		label: string;
		value: string | number | null | undefined;
		/** Shown below the field, e.g. when an environment variable locks it. */
		hint?: string;
	};

	let { label, value = $bindable(), hint, ...inputProps }: Props = $props();
</script>

<label class="field">
	<span>{label}</span>
	<input class="setup-input" bind:value {...inputProps} />
</label>
{#if hint}
	<p class="setup-hint">{hint}</p>
{/if}

<style>
	.field {
		display: grid;
		gap: 0.45rem;
		min-width: 0;
	}

	.field span {
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	.setup-input {
		width: 100%;
		min-width: 0;
		min-height: 2.4rem;
		padding: 0.5rem 0.75rem;
		border: 0;
		border-radius: 0.5rem;
		background: #303030;
		color: var(--pixel-text);
		font: inherit;
		font-size: 0.6rem;
	}

	.setup-input:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.setup-input:disabled {
		opacity: 0.6;
	}

	/* Global so section-level hints (not tied to one field) share the look. */
	:global(.setup-hint) {
		margin: -0.35rem 0 0;
		font-size: 0.45rem;
		color: var(--pixel-muted);
	}
</style>
