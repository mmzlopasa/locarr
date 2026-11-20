import type { Permissions, Role } from "@/api/client";

export const PERMISSION_KEYS = [
  "requestMovies",
  "requestSeries",
  "requestMusic",
  "autoApprove",
  "request4k",
  "manageLibrary",
  "manageRequests",
] as const satisfies readonly (keyof Permissions)[];

export const ROLES: Role[] = ["admin", "manager", "requester"];

/** Sensible starting permissions for a role (administrators may do everything anyway). */
export function defaultPermissions(role: Role): Permissions {
  const all = role !== "requester";
  return {
    requestMovies: true,
    requestSeries: true,
    requestMusic: true,
    autoApprove: all,
    request4k: all,
    manageLibrary: all,
    manageRequests: all,
  };
}
