import { EditIcon, PlusIcon } from "lucide-react";
import { useState } from "react";

import { Webhook, WebhookBase } from "~/lib/model/webhooks";

import { Button } from "~/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "~/components/ui/dialog";
import { ScrollArea } from "~/components/ui/scroll-area";
import { WebhookForm } from "~/components/webhooks/WebhookForm";

export function TaskWebhookDialog({
	taskId,
	webhook,
}: {
	taskId: string;
	webhook?: WebhookBase;
}) {
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger asChild>
				{webhook ? (
					<Button variant="ghost" size="icon">
						<EditIcon />
					</Button>
				) : (
					<Button variant="ghost" startContent={<PlusIcon />}>
						Add Webhook
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="gap-0 p-0">
				<DialogHeader className="p-8 pb-0">
					<DialogTitle>
						{webhook ? "Update Task Webhook" : "Add Webhook To Task"}
					</DialogTitle>

					<DialogDescription>
						Webhooks are used to run the task when an event is received.
					</DialogDescription>
				</DialogHeader>

				<ScrollArea className="h-[600px]">
					<WebhookForm
						hideTitle
						onContinue={() => setOpen(false)}
						webhook={{ workflowName: taskId, ...webhook } as Webhook}
					/>
				</ScrollArea>
			</DialogContent>
		</Dialog>
	);
}
