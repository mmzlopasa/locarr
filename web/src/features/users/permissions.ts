import type { Permissions, Role, User } from "@/api/client";

export const PERMISSION_KEYS = [
  "requestMovies",
  "requestSeries",
  "requestMusic",
  "request4k",
  "autoApprove",
  "autoApproveMovies",
  "autoApproveSeries",
  "autoApproveMusic",
  "autoApprove4k",
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
    autoApproveMovies: false,
    autoApproveSeries: false,
    autoApproveMusic: false,
    autoApprove4k: all,
  };
}

/** May see and change the library (movies, series, music, activity, search). */
export const canManageLibrary = (u: User | null | undefined) => !!u && (u.role === "admin" || u.permissions.manageLibrary);

/** May approve and decline other users' requests and handle issues. */
export const canManageRequests = (u: User | null | undefined) => !!u && (u.role === "admin" || u.permissions.manageRequests);
