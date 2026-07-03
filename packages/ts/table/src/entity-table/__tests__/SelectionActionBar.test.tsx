import { ModalProvider, TooltipProvider } from '@bbux/ui';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BulkActionsMenu } from '../SelectionActionBar';
import { shouldOpenBulkMenu } from '../EntitySchemaTable';
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

// The ⌘/Ctrl+K shortcut lives in EntitySchemaTable's document keydown listener,
// which is impractical to mount cleanly under jsdom (react-router + outlet +
// schema + virtualizer scaffolding not present in this harness). Instead we
// unit-test the extracted decision predicate that the listener calls — the SAME
// code path, not a reimplementation.
describe('shouldOpenBulkMenu (⌘K predicate)', () => {
  it('fires for Meta+K with a selection', () => {
    expect(shouldOpenBulkMenu({ metaKey: true, ctrlKey: false, key: 'k' }, 2)).toBe(true);
  });
  it('fires for Ctrl+K and is case-insensitive on the key', () => {
    expect(shouldOpenBulkMenu({ metaKey: false, ctrlKey: true, key: 'K' }, 1)).toBe(true);
  });
  it('does NOT fire when nothing is selected', () => {
    expect(shouldOpenBulkMenu({ metaKey: true, ctrlKey: false, key: 'k' }, 0)).toBe(false);
  });
  it('does NOT fire without the modifier or for other keys', () => {
    expect(shouldOpenBulkMenu({ metaKey: false, ctrlKey: false, key: 'k' }, 3)).toBe(false);
    expect(shouldOpenBulkMenu({ metaKey: true, ctrlKey: false, key: 'a' }, 3)).toBe(false);
  });
});

describe('BulkActionsMenu — copy', () => {
  it('multi-select shows "Copy <label>s" and writes ALL rowIds newline-joined', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });

    const { onClose } = renderMenu(ROWS, {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      copyLabel: 'SOW code',
    });

    const item = await screen.findByText('Copy SOW codes');
    await user.click(item);

    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith('1\n2\n3');
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it('single-select shows "Copy <label>" using copyLabel and copies the one id', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });

    renderMenu(ROWS.slice(0, 1), {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      copyLabel: 'SOW code',
    });

    const item = await screen.findByText('Copy SOW code');
    await user.click(item);

    expect(writeText).toHaveBeenCalledWith('1');
  });

  it('defaults the label to "ID"/"IDs" when copyLabel is omitted', async () => {
    renderMenu(ROWS.slice(0, 1), {
      entityNoun: 'person',
      nameOf: (r) => r.name,
    });
    expect(await screen.findByText('Copy ID')).toBeInTheDocument();

    renderMenu(ROWS, {
      entityNoun: 'person',
      nameOf: (r) => r.name,
    });
    expect(await screen.findByText('Copy IDs')).toBeInTheDocument();
  });
});

describe('BulkActionsMenu — single-row group merge (Fix 3)', () => {
  it('renders built-in Copy AND customActions under the SAME (single) group', async () => {
    renderMenu(ROWS.slice(0, 1), {
      entityNoun: 'person',
      nameOf: (r) => r.name,
      customActions: () => [{ label: 'Duplicate row', onSelect: vi.fn() }],
    });

    const copy = await screen.findByText('Copy ID');
    const custom = await screen.findByText('Duplicate row');

    // Both items must live inside the SAME cmdk group element — no extra empty
    // group between the built-ins and the custom action.
    const copyGroup = copy.closest('[cmdk-group]');
    const customGroup = custom.closest('[cmdk-group]');
    expect(copyGroup).not.toBeNull();
    expect(copyGroup).toBe(customGroup);
  });
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
