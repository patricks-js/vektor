import * as stylex from '@stylexjs/stylex'
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Badge, Button, Empty, ErrorState, Loading, Panel, SelectField, StatusDot, TextField, layout } from '../../components/ui/ui'
import type { Resource, ResourceKind } from '../../lib/api/client'
import { useResources } from '../../lib/api/queries'
import { displayName } from './resourceMetadata'

const s = stylex.create({
  scroll: {
    overflowX: 'auto', minWidth: 0, borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-1)',
  },
  table: { borderCollapse: 'collapse', minWidth: 460, width: '100%', textAlign: 'start', tableLayout: 'auto' },
  th: {
    color: 'var(--vk-text-secondary)', fontSize: 12, lineHeight: '16px', backgroundColor: 'var(--vk-surface-2)',
    textAlign: 'start', paddingBlock: 8, paddingInline: 14, fontWeight: 500,
    borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
  },
  row: {
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
    transition: 'background-color 150ms',
  },
  td: {
    borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)',
    paddingBlock: 11, paddingInline: 14, minHeight: 44, verticalAlign: 'middle',
    overflowWrap: 'anywhere', maxWidth: 260, fontSize: 13,
  },
  nameCell: { minWidth: 164, textAlign: 'start', fontWeight: 500 },
  rowSelected: { backgroundColor: 'var(--vk-surface-2)' },
  sort: {
    display: 'inline-flex', gap: 6, alignItems: 'center', paddingBlock: 2, paddingInline: 0,
    minHeight: { default: 28, '@media (pointer: coarse)': 44 },
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    borderWidth: 0, backgroundColor: 'transparent', fontWeight: 500, cursor: 'pointer',
  },
  link: {
    color: 'var(--vk-text-primary)', textDecoration: { default: 'none', ':hover': 'underline' },
    textUnderlineOffset: 4, display: 'inline-flex', minHeight: { default: 24, '@media (pointer: coarse)': 44 },
    alignItems: 'center', fontWeight: 600,
  },
  filters: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 170px), 1fr))', gap: 12, maxWidth: 520 },
})

export type ResourceColumn = { label: string; cell: (resource: Resource) => ReactNode }
type Sort = 'name' | 'state'
const pageSize = 6

export function ResourceTable({ kind, title, columns, selected, href, empty }: {
  kind: ResourceKind
  title: string
  columns: ResourceColumn[]
  selected?: string
  href?: (id: string) => string
  empty?: string
}) {
  const query = useResources(kind)
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [sort, setSort] = useState<{ by: Sort; ascending: boolean }>({ by: 'name', ascending: true })
  const [page, setPage] = useState(0)
  const term = search.trim().toLocaleLowerCase()
  const items = (query.data ?? []).filter(resource => {
    const match = `${displayName(resource)} ${resource.id} ${resource.external_key}`.toLocaleLowerCase().includes(term)
    return match && (filter === 'all' || resource.active === (filter === 'active'))
  })
  items.sort((a, b) => {
    const byState = sort.by === 'state' ? Number(b.active) - Number(a.active) : 0
    const byName = displayName(a).localeCompare(displayName(b), 'pt-BR', { numeric: true }) || a.id.localeCompare(b.id)
    return (byState || byName) * (sort.ascending ? 1 : -1)
  })
  const pages = Math.ceil(items.length / pageSize)
  const currentPage = Math.min(page, Math.max(0, pages - 1))
  const visible = items.slice(currentPage * pageSize, (currentPage + 1) * pageSize)

  function sortHeader(label: string, column: Sort) {
    const active = sort.by === column
    const Icon = active ? (sort.ascending ? ArrowUp : ArrowDown) : ArrowUpDown
    return <th scope="col" {...stylex.props(s.th)} aria-sort={active ? (sort.ascending ? 'ascending' : 'descending') : undefined}>
      <button type="button" {...stylex.props(s.sort)} onClick={() => {
        setSort({ by: column, ascending: active ? !sort.ascending : true })
        setPage(0)
      }}>{label}<Icon size={13} aria-hidden /></button>
    </th>
  }

  return (
    <Panel title={title} subtitle={href ? 'Clique em um recurso para visualizar o histórico detalhado.' : undefined}>
      {query.isPending && <Loading />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      {query.data && (query.data.length === 0 ? <Empty>{empty ?? 'Nenhum recurso encontrado.'}</Empty> : <>
        <div {...stylex.props(s.filters)}>
          <TextField label="Buscar recurso" placeholder="Filtrar..." value={search} onChange={event => { setSearch(event.currentTarget.value); setPage(0) }} type="search" />
          <SelectField label="Estado do inventário" value={filter} onChange={event => { setFilter(event.currentTarget.value); setPage(0) }}>
            <option value="all">Todos</option>
            <option value="active">Ativos</option>
            <option value="inactive">Inativos</option>
          </SelectField>
        </div>
        {visible.length === 0 ? <Empty>Nenhum recurso corresponde aos filtros.</Empty> : <>
          <p {...stylex.props(layout.small)}>Deslize ou use as setas na tabela para ver as colunas quando necessário.</p>
          <div tabIndex={0} role="region" aria-label={`Tabela de ${title}`} {...stylex.props(s.scroll)}>
            <table {...stylex.props(s.table)} aria-label={title}>
              <thead><tr>
                {sortHeader('Recurso', 'name')}
                {sortHeader('Inventário', 'state')}
                {columns.map(column => <th key={column.label} scope="col" {...stylex.props(s.th)}>{column.label}</th>)}
              </tr></thead>
              <tbody>{visible.map(resource => (
                <tr key={resource.id} {...stylex.props(s.row, resource.id === selected && s.rowSelected)}>
                  <th scope="row" {...stylex.props(s.td, s.nameCell)}>
                    {href
                      ? <Link {...stylex.props(s.link)} to={href(resource.id)} aria-current={selected === resource.id ? 'true' : undefined}>{displayName(resource)}</Link>
                      : <span>{displayName(resource)}</span>}
                    <div {...stylex.props(layout.technical, layout.muted)}>{resource.id}</div>
                  </th>
                  <td {...stylex.props(s.td)}>
                    <Badge tone={resource.active ? 'success' : 'neutral'} icon={<StatusDot tone={resource.active ? 'success' : 'neutral'} />}>
                      {resource.active ? 'Ativo' : 'Inativo'}
                    </Badge>
                  </td>
                  {columns.map(column => <td key={column.label} {...stylex.props(s.td)}>{column.cell(resource)}</td>)}
                </tr>
              ))}</tbody>
            </table>
          </div>
          {pages > 1 && <div {...stylex.props(layout.split)}>
            <p {...stylex.props(layout.small)}>Página {currentPage + 1} de {pages} · {items.length} recursos</p>
            <div {...stylex.props(layout.row)}>
              <Button aria-label="Página anterior" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}><ChevronLeft size={16} aria-hidden /></Button>
              <Button aria-label="Próxima página" disabled={currentPage + 1 === pages} onClick={() => setPage(currentPage + 1)}><ChevronRight size={16} aria-hidden /></Button>
            </div>
          </div>}
        </>}
      </>)}
    </Panel>
  )
}
