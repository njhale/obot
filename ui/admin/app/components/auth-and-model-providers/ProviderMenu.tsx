import { EllipsisVerticalIcon } from "lucide-react";

import { AuthProvider, ModelProvider } from "~/lib/model/providers";

import { ProviderDeconfigure } from "~/components/auth-and-model-providers/ProviderDeconfigure";
import { Button } from "~/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";

export function ProviderMenu({
	provider,
}: {
	provider: ModelProvider | AuthProvider;
}) {
	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="ghost" size="icon">
					<EllipsisVerticalIcon />
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent className="w-56" side="left" align="start">
				<DropdownMenuGroup>
					<ProviderDeconfigure provider={provider} />
				</DropdownMenuGroup>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
