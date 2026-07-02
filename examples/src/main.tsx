import { ModalProvider, TooltipProvider } from '@bbux/ui';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import './index.css';

// Providers the shared components expect: TanStack Query (FilterChips/tags),
// TooltipProvider (toolbar/close tooltips), ModalProvider (row-actions menu +
// confirm-delete), and a Router (EntityTable's row detail <Link>).
const queryClient = new QueryClient();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <ModalProvider>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </ModalProvider>
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);
