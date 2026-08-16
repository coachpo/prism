import { useEffect, useRef } from "react"
import { QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { createRewriteQueryClient, createRewriteRouter } from "@/app/index"
import { RoutedAuthProvider } from "@/app/router/appRouter"
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance"
import { clearSharedReferenceData } from "@/lib/referenceData"

const router = createRewriteRouter()
const queryClient = createRewriteQueryClient()

function App() {
  const previousEpochRef = useRef(authSessionCoordinator.getEpoch())

  useEffect(() => {
    return authSessionCoordinator.subscribe(() => {
      const epoch = authSessionCoordinator.getEpoch()
      if (epoch === previousEpochRef.current) return
      previousEpochRef.current = epoch
      // Protected query/cache owners must not carry last-good data across an
      // auth boundary. The coordinator's epoch signal aborts in-flight API
      // work; this removes completed query/reference snapshots immediately.
      void queryClient.cancelQueries()
      queryClient.removeQueries()
      clearSharedReferenceData()
    })
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <RoutedAuthProvider>
        <RouterProvider router={router} />
      </RoutedAuthProvider>
    </QueryClientProvider>
  )
}

export default App
