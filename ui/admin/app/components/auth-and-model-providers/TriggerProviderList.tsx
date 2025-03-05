// ui/admin/app/components/auth-and-model-providers/TriggerProviderLists.tsx
import { CircleCheckIcon, CircleSlashIcon } from "lucide-react";
import { Link } from "react-router";

import { TriggerProvider } from "~/lib/model/providers";

import { ProviderConfigure } from "~/components/auth-and-model-providers/ProviderConfigure";
import { ProviderIcon } from "~/components/auth-and-model-providers/ProviderIcon";
import { ProviderMenu } from "~/components/auth-and-model-providers/ProviderMenu";
import { Badge } from "~/components/ui/badge";
import { Card, CardContent, CardHeader } from "~/components/ui/card";

export function TriggerProviderList({
    triggerProviders,
}: {
    triggerProviders: TriggerProvider[];
}) {
    return (
        <div className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                {triggerProviders.map((triggerProvider) => (
                    <Card key={triggerProvider.id}>
                        <CardHeader className="flex flex-row items-center justify-between pb-4 pt-2">
                            {triggerProvider.configured && (
                                <div className="flex flex-row items-center gap-2">
                                    <ProviderMenu provider={triggerProvider} />
                                </div>
                            )}
                            {!triggerProvider.configured && (
                                <div className="flex flex-row items-center gap-2" />
                            )}
                        </CardHeader>
                        <CardContent className="flex flex-col items-center gap-4">
                            <Link to={triggerProvider.link ?? ""}>
                                <ProviderIcon provider={triggerProvider} size="lg" />
                            </Link>
                            <div className="text-center text-lg font-semibold">
                                {triggerProvider.name}
                            </div>

                            <Badge className="pointer-events-none" variant="outline">
                                {triggerProvider.configured ? (
                                    <span className="flex items-center gap-1">
                                        <CircleCheckIcon size={18} className="text-success" />
                                        Configured
                                    </span>
                                ) : (
                                    <span className="flex items-center gap-1">
                                        <CircleSlashIcon size={18} className="text-destructive" />
                                        Not Configured
                                    </span>
                                )}
                            </Badge>
                            <ProviderConfigure provider={triggerProvider} />
                        </CardContent>
                    </Card>
                ))}
            </div>
        </div>
    );
}