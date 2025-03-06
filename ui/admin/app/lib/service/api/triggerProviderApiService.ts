import { TriggerProvider, ProviderConfig } from "~/lib/model/providers";
import { ApiRoutes } from "~/lib/routers/apiRoutes";
import { request } from "~/lib/service/api/primitives";

const getTriggerProviders = async () => {
    const res = await request<{ items: TriggerProvider[] }>({
        url: ApiRoutes.triggerProviders.getTriggerProviders().url,
        errorMessage: "Failed to get supported trigger providers.",
    });

    return res.data.items ?? ([] as TriggerProvider[]);
};
getTriggerProviders.key = () => 
    ({ url: ApiRoutes.triggerProviders.getTriggerProviders().path }) as const;

const getTriggerProviderById = async (providerKey: string) => {
    const res = await request<TriggerProvider>({
        url: ApiRoutes.triggerProviders.getTriggerProviderById(providerKey).url,
        method: "GET",
        errorMessage: "Failed to get the requested trigger provider.",
    });

    return res.data;
};
getTriggerProviderById.key = (providerId?: string) => {
    if (!providerId) return null;

    return {
        url: ApiRoutes.triggerProviders.getTriggerProviderById(providerId).path,
        providerId,
    };
};

const configureTriggerProviderById = async (
    providerKey: string,
    providerConfig: ProviderConfig
) => {
    const res = await request<TriggerProvider>({
        url: ApiRoutes.triggerProviders.configureTriggerProviderById(providerKey).url,
        method: "POST",
        data: providerConfig,
        errorMessage: "Failed to configure the requested trigger provider.",
    });

    return res.data;
};

const revealTriggerProviderById = async (providerKey: string) => {
    const res = await request<ProviderConfig>({
        url: ApiRoutes.triggerProviders.revealTriggerProviderById(providerKey).url,
        method: "POST",
        errorMessage: "Failed to reveal configuration for the requested trigger provider.",
        toastError: false,
    });

    return res.data;
};
revealTriggerProviderById.key = (providerId?: string) => {
    if (!providerId) return null;

    return {
        url: ApiRoutes.triggerProviders.revealTriggerProviderById(providerId).path,
        providerId,
    };
};

const deconfigureTriggerProviderById = async (providerKey: string) => {
    const res = await request<TriggerProvider>({
        url: ApiRoutes.triggerProviders.deconfigureTriggerProviderById(providerKey).url,
        method: "POST",
        errorMessage: "Failed to deconfigure the requested trigger provider.",
    });

    return res.data;
};

export const TriggerProviderApiService = {
    getTriggerProviders,
    getTriggerProviderById,
    configureTriggerProviderById,
    revealTriggerProviderById,
    deconfigureTriggerProviderById,
};