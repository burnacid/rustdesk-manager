import { createContext, useContext, useEffect, type ReactNode } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, ApiError, type User } from "./lib/api";
import { Login } from "./pages/Login";
import { Loading } from "./components/ui";

const UserContext = createContext<User | null>(null);
export const useUser = () => useContext(UserContext)!;

// Mark the session as gone and drop everything cached for the previous user.
// Don't use queryClient.clear(): it detaches the "me" query from the AuthGate,
// so the screen would never notice the sign-out.
function signedOut(qc: QueryClient) {
  qc.setQueryData(["me"], null);
  qc.removeQueries({ predicate: (q) => q.queryKey[0] !== "me" });
}

/** Shows the login page until there is a valid session. */
export function AuthGate({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api<{ user: User }>("/auth/me"),
    retry: false,
  });

  useEffect(() => {
    // Any 401 from another request (e.g. session expired) sends us back to login.
    const onUnauthorized = () => signedOut(qc);
    window.addEventListener("rdm:unauthorized", onUnauthorized);
    return () => window.removeEventListener("rdm:unauthorized", onUnauthorized);
  }, [qc]);

  if (me.isPending) return <Loading />;
  if (!me.data || (me.error instanceof ApiError && me.error.status === 401)) {
    return <Login onLoggedIn={(user) => qc.setQueryData(["me"], { user })} />;
  }
  return <UserContext.Provider value={me.data.user}>{children}</UserContext.Provider>;
}

export function useLogout() {
  const qc = useQueryClient();
  return async () => {
    await api("/auth/logout", { method: "POST" }).catch(() => {});
    signedOut(qc);
  };
}

/** Refresh the signed-in user (e.g. after turning 2FA on). */
export function useRefreshMe() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ["me"] });
}
