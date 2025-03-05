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

const configureTriggerProvider = async (
    providerKey: string,
    providerConfig: ProviderConfig
) => {
    const res = await request<TriggerProvider>({
        url: ApiRoutes.triggerProviders.configure(providerKey).url,
        method: "POST",
        data: providerConfig,
        errorMessage: "Failed to configure the requested trigger provider.",
    });

    return res.data;
};

const revealTriggerProvider = async (providerKey: string) => {
    const res = await request<ProviderConfig>({
        url: ApiRoutes.triggerProviders.reveal(providerKey).url,
        method: "POST",
        errorMessage: "Failed to reveal configuration for the requested trigger provider.",
        toastError: false,
    });

    return res.data;
};
revealTriggerProvider.key = (providerId?: string) => {
    if (!providerId) return null;

    return {
        url: ApiRoutes.triggerProviders.reveal(providerId).path,
        providerId,
    };
};

const deconfigureTriggerProvider = async (providerKey: string) => {
    const res = await request<TriggerProvider>({
        url: ApiRoutes.triggerProviders.deconfigure(providerKey).url,
        method: "POST",
        errorMessage: "Failed to deconfigure the requested trigger provider.",
    });

    return res.data;
};

export const TriggerProviderApiService = {
    getTriggerProviders,
    getTriggerProviderById,
    configureTriggerProvider,
    revealTriggerProvider,
    deconfigureTriggerProvider,
};