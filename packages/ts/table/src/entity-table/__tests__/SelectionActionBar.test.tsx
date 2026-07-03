import { ModalProvider, TooltipProvider } from '@bbux/ui';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BulkActionsMenu } from '../SelectionActionBar';
import type { RowActionsConfig } from '../types/types';

// The bulk-actions command menu renders through @bbux/ui's ModalProvider (portal
// into document.body) and reads a QueryClient. We drive it with real user-event
// clicks against the cmdk CommandItems — the same surface a user touches.

interface Row {
  id: string;
  name: string;
}

const ROWS: Row[] = [
  { id: '1', name: 'Ada' },
  { id: '2', name: 'Alan' },
  { id: '3', name: 'Grace' },
];

function renderMenu(rows: Row[], rowActions: RowActionsConfig<Row>) {
  const onClose = vi.fn();
  const onClear = vi.fn();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const ui: ReactElement = (
    <QueryClientProvider client={qc}>
      <TooltipProvider>
        <ModalProvider>
          <BulkActionsMenu<Row>
          rows={rows}
          rowActions={rowActions}
          rowId={(r) => r.id}
          editHref={() => undefined}
          onNavigate={() => {}}
            onClear={onClear}
            onClose={onClose}
          />
        </ModalProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
  return { onClose, onClear, ...render(ui) };
}


// The Delete command item's label ("Delete 3 people…") is split across text
// nodes by the icon/count, so match on the element's normalized textContent.
async function findDeleteItem() {
  return screen.findByText((_content, el) => {
    if (!el) return false;
    const text = el.textContent ?? '';
    return el.getAttribute('cmdk-item') !== null && /Delete\s+3\s+persons/.test(text);
  });
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('BulkActionsMenu — bulk actions', () => {
  it('renders a provided bulkActions entry for a multi-row selection and calls onSelect with ALL selected rows', async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    const twoRows = ROWS.slice(0, 2);
    const { onClose, onClear } = renderMenu(twoRows, {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      bulkActions: () => [{ label: 'Email selected', onSelect }],
    });

    const item = await screen.findByText('Email selected');
    await user.click(item);

    expect(onSelect).toHaveBeenCalledTimes(1);
    // Called with every selected row.
    expect(onSelect).toHaveBeenCalledWith(twoRows);
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it('does NOT render bulkActions for a single-row selection', async () => {
    const onSelect = vi.fn();
    renderMenu(ROWS.slice(0, 1), {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      bulkActions: () => [{ label: 'Email selected', onSelect }],
    });
    // Menu has rendered (title is present) but the bulk item is not.
    await screen.findByText('Ada');
    expect(screen.queryByText('Email selected')).toBeNull();
  });
});

describe('BulkActionsMenu — single-row customActions', () => {
  it('renders customActions for a 1-row selection', async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    renderMenu(ROWS.slice(0, 1), {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      customActions: () => [{ label: 'Duplicate row', onSelect }],
    });
    const item = await screen.findByText('Duplicate row');
    await user.click(item);
    expect(onSelect).toHaveBeenCalledTimes(1);
  });
});

describe('BulkActionsMenu — batched delete', () => {
  it('calls onBulkDelete EXACTLY ONCE with the deletable rows and NOT per-row onDelete', async () => {
    const user = userEvent.setup();
    const onDelete = vi.fn().mockResolvedValue(undefined);
    const onBulkDelete = vi.fn().mockResolvedValue(undefined);
    const { onClear } = renderMenu(ROWS, {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      onDelete,
      onBulkDelete,
    });

    // Open the delete entry -> ConfirmDeleteModal stacks on top.
    const deleteItem = await findDeleteItem();
    await user.click(deleteItem);

    // Type the required "3 people" confirmation text, then confirm.
    const input = await screen.findByTestId('confirm-delete-input');
    await user.type(input, '3 persons');
    const confirmBtn = await screen.findByTestId('confirm-delete-submit');
    await user.click(confirmBtn);

    await waitFor(() => expect(onBulkDelete).toHaveBeenCalledTimes(1));
    expect(onBulkDelete).toHaveBeenCalledWith(ROWS);
    // The per-row handler must NOT be invoked in the batched path.
    expect(onDelete).not.toHaveBeenCalled();
    await waitFor(() => expect(onClear).toHaveBeenCalledTimes(1));
  });

  it('falls back to per-row onDelete via Promise.all when onBulkDelete is absent', async () => {
    const user = userEvent.setup();
    const onDelete = vi.fn().mockResolvedValue(undefined);
    renderMenu(ROWS, {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      onDelete,
    });

    const deleteItem = await findDeleteItem();
    await user.click(deleteItem);
    const input = await screen.findByTestId('confirm-delete-input');
    await user.type(input, '3 persons');
    const confirmBtn = await screen.findByTestId('confirm-delete-submit');
    await user.click(confirmBtn);

    await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(3));
  });
});
