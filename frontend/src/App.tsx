import { QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { createRewriteQueryClient, createRewriteRouter } from "@/app/index"
import { RoutedAuthProvider } from "@/app/router/appRouter"

const router = createRewriteRouter()
const queryClient = createRewriteQueryClient()

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RoutedAuthProvider>
        <RouterProvider router={router} />
      </RoutedAuthProvider>
    </QueryClientProvider>
  )
}

export default App
