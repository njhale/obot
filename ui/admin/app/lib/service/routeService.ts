import queryString from "query-string";
import { $path, type Routes, type RoutesWithParams } from "safe-routes";
import { ZodNull, ZodSchema, ZodType, z } from "zod";

// note: If you see a linter error related to `Routes`, or `RoutesWithParams`,
// it's probably because you need to run `pnpm run dev` or `pnpm run build`
// these types are generated by safe-routes, and are used to provide type
// safety when navigating through the app

const QuerySchemas = {
	agentSchema: z.object({
		threadId: z.string().nullish(),
		from: z.string().nullish(),
	}),
	threadsListSchema: z.object({
		agentId: z.string().nullish(),
		userId: z.string().nullish(),
		taskId: z.string().nullish(),
		from: z.enum(["tasks", "agents", "users"]).nullish().catch(null),
		createdStart: z.string().nullish(),
		createdEnd: z.string().nullish(),
	}),
	taskSchema: z.object({
		threadId: z.string().nullish(),
	}),
	tasksSchema: z.object({
		agentId: z.string().nullish(),
		userId: z.string().nullish(),
		taskId: z.string().nullish(),
		createdStart: z.string().nullish(),
		createdEnd: z.string().nullish(),
	}),
	usersSchema: z.object({ userId: z.string().optional() }),
} as const;

function parseQuery<T extends ZodType>(search: string, schema: T) {
	if (schema instanceof ZodNull) return null;

	const obj = queryString.parse(search);
	const { data, success } = schema.safeParse(obj);

	if (!success) {
		console.error("Failed to parse query params", search);
		return null;
	}

	return data;
}

const exactRegex = (path: string) => new RegExp(`^${path}$`);

type RouteHelper = {
	regex: RegExp;
	path: keyof Routes;
	schema: ZodSchema;
};

export const RouteHelperMap = {
	"": {
		regex: exactRegex($path("")),
		path: "/",
		schema: z.null(),
	},
	"/": {
		regex: exactRegex($path("/")),
		path: "/",
		schema: z.null(),
	},
	"/agents": {
		regex: exactRegex($path("/agents")),
		path: "/agents",
		schema: z.null(),
	},
	"/agents/:id": {
		regex: exactRegex($path("/agents/:id", { id: "(.+)" })),
		path: "/agents/:id",
		schema: QuerySchemas.agentSchema,
	},
	"/auth-providers": {
		regex: exactRegex($path("/auth-providers")),
		path: "/auth-providers",
		schema: z.null(),
	},
	"/debug": {
		regex: exactRegex($path("/debug")),
		path: "/debug",
		schema: z.null(),
	},
	"/chat-threads": {
		regex: exactRegex($path("/chat-threads")),
		path: "/chat-threads",
		schema: QuerySchemas.threadsListSchema,
	},
	"/chat-threads/:id": {
		regex: exactRegex($path("/chat-threads/:id", { id: "(.+)" })),
		path: "/chat-threads/:id",
		schema: z.null(),
	},
	"/home": {
		regex: exactRegex($path("/home")),
		path: "/home",
		schema: z.null(),
	},
	"/model-providers": {
		regex: exactRegex($path("/model-providers")),
		path: "/model-providers",
		schema: z.null(),
	},
	"/tools": {
		regex: exactRegex($path("/tools")),
		path: "/tools",
		schema: z.null(),
	},
	"/users": {
		regex: exactRegex($path("/users")),
		path: "/users",
		schema: QuerySchemas.usersSchema,
	},
	"/tasks": {
		regex: exactRegex($path("/users")),
		path: "/users",
		schema: QuerySchemas.tasksSchema,
	},
	"/tasks/:id": {
		regex: exactRegex($path("/tasks/:id", { id: "(.+)" })),
		path: "/tasks/:id",
		schema: QuerySchemas.taskSchema,
	},
	"/task-runs": {
		regex: exactRegex($path("/task-runs")),
		path: "/task-runs",
		schema: QuerySchemas.threadsListSchema,
	},
	"/task-runs/:id": {
		regex: exactRegex($path("/task-runs/:id", { id: "(.+)" })),
		path: "/task-runs/:id",
		schema: z.null(),
	},
} satisfies Record<keyof Routes, RouteHelper>;

type QueryInfo<T extends keyof Routes> = z.infer<
	(typeof RouteHelperMap)[T]["schema"]
>;

type PathInfo<T extends keyof RoutesWithParams> = {
	[key in keyof Routes[T]["params"]]: string;
};

type RoutePathInfo<T extends keyof Routes> = T extends keyof RoutesWithParams
	? PathInfo<T>
	: unknown;

export type RouteInfo<T extends keyof Routes = keyof Routes> = {
	path: T;
	query: QueryInfo<T> | null;
	pathParams: RoutePathInfo<T>;
};

function convertToStringObject(obj: object) {
	return Object.fromEntries(
		Object.entries(obj).map(([key, value]) => [key, String(value)])
	);
}

function getRouteInfo<T extends keyof Routes>(
	path: T,
	url: URL,
	params: Record<string, string | undefined>
): RouteInfo<T> {
	const helper = RouteHelperMap[path];

	return {
		path,
		query: parseQuery(url.search, helper.schema),
		pathParams: convertToStringObject(params) as RoutePathInfo<T>,
	};
}

// note: this is a ✨fancy✨ way of saying
// type UnknownRouteInfo = RouteInfo<keyof Routes>
// but it is needed to discriminate between the different routes
// via the `path` property
type UnknownRouteInfo = {
	[key in keyof Routes]: RouteInfo<key>;
}[keyof Routes];

function getUnknownRouteInfo(
	url: URL,
	params: Record<string, string | undefined>
) {
	for (const route of Object.values(RouteHelperMap)) {
		if (route.regex.test(url.pathname))
			return {
				path: route.path,
				query: parseQuery(url.search, route.schema as ZodSchema),
				pathParams: convertToStringObject(params),
			} as UnknownRouteInfo;
	}

	return null;
}

export type RouteQueryParams<T extends keyof typeof QuerySchemas> = z.infer<
	(typeof QuerySchemas)[T]
>;

const getQueryParams = <T extends keyof Routes>(path: T, search: string) =>
	parseQuery(search, RouteHelperMap[path].schema) as RouteInfo<T>["query"];

export const RouteService = {
	schemas: QuerySchemas,
	getUnknownRouteInfo,
	getRouteInfo,
	getQueryParams,
};
