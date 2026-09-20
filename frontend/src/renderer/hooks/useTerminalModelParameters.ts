import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../lib/api-client";

export function terminalModelParametersQueryKey(sessionId: string, generation: string) {
	return ["terminal-model-parameters", sessionId, generation] as const;
}

export function useTerminalModelParameters(sessionId: string, generation: string, enabled: boolean) {
	return useQuery({
		queryKey: terminalModelParametersQueryKey(sessionId, generation),
		enabled,
		staleTime: 0,
		refetchOnMount: "always",
		refetchOnWindowFocus: false,
		refetchOnReconnect: false,
		refetchInterval: false,
		retry: false,
		// Deliberately share an in-flight request across quick re-entry. The
		// daemon bounds the read; a late response stays scoped to its generation.
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/sessions/{sessionId}/terminal-model-parameters", {
				params: { path: { sessionId } },
			});
			if (error || !data) throw new Error("Saved model parameters could not be confirmed");
			return data;
		},
	});
}
