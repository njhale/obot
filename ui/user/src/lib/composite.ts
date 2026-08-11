import type { CompositeComponent, MCPServer } from '$lib/services/user/types';

/**
 * A composite server or catalog entry carries its component references in the manifest and the
 * resolved view of those references alongside it. Only the reference is stored; the component's
 * configuration is resolved from its source when the parent is read.
 */
export interface CompositeParent {
	components?: CompositeComponent[];
}

/** Identifies a component within its composite. */
export function getComponentId(component: {
	catalogEntryID?: string;
	mcpServerID?: string;
}): string {
	return component.catalogEntryID || component.mcpServerID || '';
}

/** Indexes a composite's resolved components by component ID. */
export function componentsById(parent: CompositeParent | undefined | null) {
	const byID = new Map<string, CompositeComponent>();
	for (const component of parent?.components ?? []) {
		const id = getComponentId(component);
		if (id) byID.set(id, component);
	}
	return byID;
}

/** Returns a composite's resolved component by ID, if the parent has been loaded. */
export function getComponent(
	parent: CompositeParent | undefined | null,
	componentId: string
): CompositeComponent | undefined {
	return parent?.components?.find((c) => getComponentId(c) === componentId);
}

/**
 * Returns a component's source configuration. Undefined when the parent has not been loaded or
 * the reference no longer resolves, so callers should fall back to the component ID for display.
 */
export function getComponentManifest(
	parent: CompositeParent | undefined | null,
	componentId: string
): MCPServer | undefined {
	return getComponent(parent, componentId)?.manifest;
}
