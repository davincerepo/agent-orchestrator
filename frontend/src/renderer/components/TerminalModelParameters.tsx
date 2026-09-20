import { useTerminalModelParameters } from "../hooks/useTerminalModelParameters";

export function TerminalModelParameters({ sessionId, generation, enabled }: {
	sessionId: string;
	generation: string;
	enabled: boolean;
}) {
	const query = useTerminalModelParameters(sessionId, generation, enabled);
	// Never present an earlier successful read as confirmed after a failed
	// refresh, or while this visit is still checking native history.
	const data = enabled && !query.isError && !query.isFetching ? query.data : undefined;
	if (data?.supported === false) return null;
	const status = !enabled ? "Waiting for daemon" : query.isFetching ? "Checking saved parameters…"
		: query.isError ? "Unconfirmed · reopen terminal to retry" : "Saved parameters";
	const fast = data?.serviceTier === "priority" ? "On" : data?.serviceTier === "default" ? "Off" : "Unknown";
	return (
		<div
			className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-border px-3 py-1 text-xs text-muted-foreground"
			data-testid="terminal-model-parameters"
			role="status"
			title="Last parameters saved by Codex. Changes not yet saved by the terminal may appear on your next visit."
		>
			<span>{status}</span>
			<span className="break-all">Model: {data?.model || "Unknown"}</span>
			<span>Effort: {data?.reasoningEffort || "Unknown"}</span>
			<span>Fast: {fast}</span>
		</div>
	);
}
