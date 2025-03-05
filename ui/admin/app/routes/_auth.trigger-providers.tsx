import { MetaFunction } from "react-router";
import { TriggerProvider } from "~/lib/model/providers";
import { RouteHandle } from "~/lib/service/routeHandles";
import { WarningAlert } from "~/components/composed/WarningAlert";
import { useTriggerProviders } from "~/hooks/trigger-providers/useTriggerProviders";
import { CommonTriggerProviderIds } from "~/components/auth-and-model-providers/constants";
import { TriggerProviderList } from "~/components/auth-and-model-providers/TriggerProviderList";

const sortTriggerProviders = (triggerProviders: TriggerProvider[]) => {
    return [...triggerProviders].sort((a, b) => {
        const preferredOrder: string[] = [
            CommonTriggerProviderIds.SLACK,
        ];
        const aIndex = preferredOrder.indexOf(a.id);
        const bIndex = preferredOrder.indexOf(b.id);

        // If both providers are in preferredOrder, sort by their order
        if (aIndex !== -1 && bIndex !== -1) {
            return aIndex - bIndex;
        }

        // If only a is in preferredOrder, it comes first
        if (aIndex !== -1) return -1;
        // If only b is in preferredOrder, it comes first
        if (bIndex !== -1) return 1;

        // For all other providers, sort alphabetically by name
        return a.name.localeCompare(b.name);
    });
};

export default function TriggerProviders() {
    const { configured: triggerProviderConfigured, triggerProviders } = useTriggerProviders();
    const sortedTriggerProviders = sortTriggerProviders(triggerProviders);

    return (
        <div>
            <div className="relative px-8 pb-8">
                <div className="sticky top-0 z-10 flex flex-col gap-4 bg-background py-8">
                    <div className="flex items-center justify-between">
                        <h2 className="mb-0 pb-0">Trigger Providers</h2>
                    </div>
                    {triggerProviderConfigured ? (
                        <div className="h-16 w-full" />
                    ) : (
                        <WarningAlert
                            title="No Trigger Providers Configured!"
                            description="Configure a Trigger Provider to enable automated task execution based on external events."
                        />
                    )}
                </div>

                <div className="flex h-full flex-col gap-8 overflow-hidden">

                    <TriggerProviderList triggerProviders={sortedTriggerProviders} />
                </div>
            </div>
        </div>
    );
}

export const handle: RouteHandle = {
    breadcrumb: () => [{ content: "Trigger Providers" }],
};

export const meta: MetaFunction = () => {
    return [{ title: `Obot • Trigger Providers` }];
};
