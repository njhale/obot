import useSWR from "swr";
import { TriggerProviderApiService } from "~/lib/service/api/triggerProviderApiService";

export function useTriggerProviders() {
	const { data: triggerProviders, ...rest } = useSWR(
		TriggerProviderApiService.getTriggerProviders.key(),
		() => TriggerProviderApiService.getTriggerProviders(),
		{ fallbackData: [] }
	);
	const configured =
		triggerProviders?.some((triggerProvider) => triggerProvider.configured) ?? false;

	return { configured, triggerProviders, ...rest };
}
