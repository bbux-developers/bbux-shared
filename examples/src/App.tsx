import { EntityTable, FilterChips, type RowActionsConfig } from '@bbux/table';
import type { ActiveFilters, FilterField, ListSchema, TableSettings } from '@bbux/types';
import { defaultTableSettings } from '@bbux/types';
import { ListHeader } from '@bbux/ui';
import { CircleDot, Layers, Mail, Table, Users } from 'lucide-react';
import { useState } from 'react';

// Standalone playground for the shared @bbux/* components. It renders the real
// EntityTable + ListHeader + FilterChips against static sample data so you can
// eyeball: selection highlight (click a row's checkbox), keyboard nav, sort
// header clicks, grouping (the "Group by team" button), and the row "⋯" menu.
//
// NOTE: EntityTable is a view layer — a real app applies sort/filter to the
// data and feeds the result in. Here the rows are static, so the sort arrows +
// filter chips update state but don't re-order/filter the sample rows; grouping
// DOES take effect (the table groups the rows it's given).

interface Person {
  id: string;
  name: string;
  team: string;
  status: string;
  amount: string;
}

const schema: ListSchema = {
  columns: [
    { key: 'name', label: 'Name', type: 'text', sortable: true, pinned: 'left', primary: true },
    { key: 'team', label: 'Team', type: 'text', sortable: true },
    { key: 'status', label: 'Status', type: 'text', sortable: true },
    // Right-pinned: stays stuck to the right edge (just inside the actions
    // column) during horizontal scroll — mirror of the left-pinned Name.
    { key: 'amount', label: 'Amount', type: 'text', sortable: true, pinned: 'right' },
  ],
};

const PEOPLE: Person[] = [
  { id: '1', name: 'Ada Lovelace', team: 'Platform', status: 'Active', amount: '$1,200' },
  { id: '2', name: 'Alan Turing', team: 'Platform', status: 'Active', amount: '$980' },
  { id: '3', name: 'Grace Hopper', team: 'Design', status: 'Invited', amount: '$640' },
  { id: '4', name: 'Katherine Johnson', team: 'Design', status: 'Active', amount: '$1,500' },
  { id: '5', name: 'Margaret Hamilton', team: 'Ops', status: 'Suspended', amount: '$0' },
  { id: '6', name: 'Barbara Liskov', team: 'Ops', status: 'Active', amount: '$2,100' },
];

const filterFields: FilterField[] = [
  { kind: 'text', key: 'name', label: 'Name', icon: Users, iconClassName: 'text-text-muted' },
  { kind: 'text', key: 'team', label: 'Team', icon: Layers, iconClassName: 'text-text-muted' },
  // Static enum filter with per-value colours: the picker shows a coloured
  // dot beside each option, and slicing the chart by Status colours each bar
  // to match (Active→green, Invited→amber, Suspended→red).
  {
    kind: 'static',
    key: 'status',
    label: 'Status',
    icon: CircleDot,
    iconClassName: 'text-text-muted',
    values: [
      { value: 'Active', label: 'Active', color: 'success' },
      { value: 'Invited', label: 'Invited', color: 'warning' },
      { value: 'Suspended', label: 'Suspended', color: 'danger' },
    ],
  },
];

export default function App() {
  const [settings, setSettings] = useState<TableSettings>(() => defaultTableSettings(schema));
  const [filters, setFilters] = useState<ActiveFilters>({});
  // Toggle the leading selection-checkbox column on/off (the codes list runs
  // with selection off — no checkboxes, no bulk bar).
  const [selectable, setSelectable] = useState(true);

  const rowActions: RowActionsConfig<Person> = {
    entityNoun: 'person',
    copyLabel: 'person ID',
    nameOf: (p) => p.name,
    onDelete: (p) => {
      window.alert(`(demo) delete ${p.name}`);
      return Promise.resolve();
    },
    // Custom bulk action for a multi-row selection — receives every selected row.
    bulkActions: (rows) => [
      {
        label: `Email ${rows.length} selected…`,
        icon: <Mail aria-hidden />,
        onSelect: (selected) =>
          window.alert(`(demo) email ${selected.map((p) => p.name).join(', ')}`),
      },
    ],
    // Batched delete — called ONCE for the whole selection instead of per-row.
    onBulkDelete: (rows) => {
      window.alert(`(demo) bulk delete ${rows.length}: ${rows.map((p) => p.name).join(', ')}`);
      return Promise.resolve();
    },
  };

  return (
    <div className="flex h-svh flex-col bg-background text-text">
      <ListHeader left={<FilterChips fields={filterFields} active={filters} onChange={setFilters} />}>
        <button
          type="button"
          onClick={() =>
            setSettings((s) => ({ ...s, groupBy: s.groupBy === 'team' ? null : 'team' }))
          }
          className="rounded-md border border-border px-2 py-1 text-text-muted text-xs hover:text-text"
        >
          {settings.groupBy ? 'Ungroup' : 'Group by team'}
        </button>
        <button
          type="button"
          onClick={() => setSelectable((s) => !s)}
          className="rounded-md border border-border px-2 py-1 text-text-muted text-xs hover:text-text"
        >
          {selectable ? 'Selection: on' : 'Selection: off'}
        </button>
      </ListHeader>

      <EntityTable<Person>
        title="People"
        schema={schema}
        rows={PEOPLE}
        total={PEOPLE.length}
        newLabel="New person"
        isLoading={false}
        error={null}
        emptyIcon={<Table />}
        emptyTitle="No people"
        emptyDescription="Nothing to show."
        rowKey={(p) => p.id}
        activeFilters={filters}
        onFiltersChange={setFilters}
        tableSettings={settings}
        onTableSettingsChange={setSettings}
        rowActions={rowActions}
        selectable={selectable}
        onLoadMore={() => {}}
        hasMore={false}
        isFetchingMore={false}
      />
    </div>
  );
}
