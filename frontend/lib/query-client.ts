import { QueryClient } from '@tanstack/react-query'

// queryClient centralizes bounded retries and refresh behavior for dashboard queries.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})
