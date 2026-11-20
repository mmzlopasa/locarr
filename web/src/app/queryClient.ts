import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError, isSessionLost } from "@/api/client";

/**
 * The app's query client. A request answered "not signed in" (an expired or
 * revoked session) calls onSessionLost, which sends the user to the sign-in page.
 */
export function createQueryClient(onSessionLost: () => void) {
  const onError = (err: unknown) => {
    if (isSessionLost(err)) onSessionLost();
  };
  return new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: {
      queries: {
        // Client errors do not get better by retrying; network blips and 5xx may.
        retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
        refetchOnWindowFocus: false,
      },
    },
  });
}
