import { QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { BrowserRouter } from "react-router-dom"
import { createRewriteQueryClient, createRewriteRouter } from "@/app"
import { RoutedAuthProvider } from "@/app/router/appRouter"

const router = createRewriteRouter()
const queryClient = createRewriteQueryClient()

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <RoutedAuthProvider>
          <RouterProvider router={router} />
        </RoutedAuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

export default App
