import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DataTable, type Column } from './DataTable'

interface Row {
  id: string
  name: string
  n: number
}
const rows: Row[] = [
  { id: 'a', name: 'alpha', n: 3 },
  { id: 'b', name: 'bravo', n: 1 },
  { id: 'c', name: 'charlie', n: 2 },
]
const cols: Column<Row>[] = [
  {
    key: 'name',
    header: 'Name',
    render: (r) => r.name,
    sort: (a, b) => a.name.localeCompare(b.name),
  },
  { key: 'n', header: 'Count', align: 'right', render: (r) => r.n, sort: (a, b) => a.n - b.n },
]

const order = () =>
  within(screen.getByRole('table'))
    .getAllByRole('row')
    .slice(1)
    .map((r) => r.textContent)

describe('DataTable', () => {
  it('sorts by a column and toggles direction', async () => {
    render(<DataTable label="t" columns={cols} rows={rows} rowKey={(r) => r.id} />)
    await userEvent.click(screen.getByRole('button', { name: 'Count' }))
    expect(order()).toEqual(['bravo1', 'charlie2', 'alpha3'])
    await userEvent.click(screen.getByRole('button', { name: 'Count' }))
    expect(order()).toEqual(['alpha3', 'charlie2', 'bravo1'])
    expect(screen.getByRole('columnheader', { name: /Count/ })).toHaveAttribute(
      'aria-sort',
      'descending',
    )
  })

  it('selects by click and keyboard', async () => {
    const onSelect = vi.fn()
    render(
      <DataTable
        label="t"
        columns={cols}
        rows={rows}
        rowKey={(r) => r.id}
        onSelect={onSelect}
        selectedKey="b"
      />,
    )
    const bravo = screen.getByText('bravo').closest('tr')!
    expect(bravo).toHaveAttribute('aria-selected', 'true')
    await userEvent.click(screen.getByText('alpha'))
    expect(onSelect).toHaveBeenLastCalledWith(rows[0])
    screen.getByText('charlie').closest('tr')!.focus()
    await userEvent.keyboard('{Enter}')
    expect(onSelect).toHaveBeenLastCalledWith(rows[2])
  })

  it('shows the empty state', () => {
    render(
      <DataTable
        label="t"
        columns={cols}
        rows={[]}
        rowKey={(r) => r.id}
        empty={<div>nothing here</div>}
      />,
    )
    expect(screen.getByText('nothing here')).toBeInTheDocument()
  })
})
