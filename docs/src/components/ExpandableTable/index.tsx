import React, {
  Children,
  Fragment,
  isValidElement,
  useEffect,
  useId,
  useRef,
  useState,
  type ComponentProps,
  type MouseEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import {MDXProvider} from '@mdx-js/react';
import styles from './styles.module.css';

type Node = ReactElement<{children?: ReactNode}>;

function elements(node: ReactNode): Node[] {
  return Children.toArray(node).filter(isValidElement) as Node[];
}

function cells(row: Node | undefined): ReactNode[] {
  return elements(row?.props.children).map((cell) => cell.props.children);
}

function text(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') {
    return String(node);
  }
  if (Array.isArray(node)) {
    return node.map(text).join('');
  }
  if (isValidElement<{children?: ReactNode}>(node)) {
    return text(node.props.children);
  }
  return '';
}

function Table({children}: ComponentProps<'table'>) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState<ReadonlySet<number>>(new Set());

  const [head, body] = elements(children);
  const header = cells(elements(head?.props.children)[0]);
  const rows = elements(body?.props.children).map(cells);
  const split = header.length - 1;
  const allOpen = rows.length > 0 && open.size === rows.length;

  useEffect(() => {
    root.current
      ?.querySelectorAll('[data-details][hidden]')
      .forEach((el) => el.setAttribute('hidden', 'until-found'));
  }, [open]);

  useEffect(() => {
    const el = root.current;
    if (!el) {
      return;
    }
    const onMatch = (event: Event) => {
      const index = Number((event.target as HTMLElement).dataset.details);
      setOpen((prev) => new Set(prev).add(index));
    };
    el.addEventListener('beforematch', onMatch);
    return () => el.removeEventListener('beforematch', onMatch);
  }, []);

  const toggle = (index: number) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(index)) {
        next.add(index);
      }
      return next;
    });

  const onRowClick = (event: MouseEvent<HTMLTableRowElement>, index: number) => {
    if ((event.target as HTMLElement).closest('a')) {
      return;
    }
    if (window.getSelection()?.toString()) {
      return;
    }
    toggle(index);
  };

  const toggleAll = () =>
    setOpen(allOpen ? new Set() : new Set(rows.map((_, i) => i)));

  return (
    <div ref={root} className={styles.root}>
      <div className={styles.toolbar}>
        <button type="button" className={styles.toggleAll} onClick={toggleAll}>
          {allOpen ? 'Collapse all' : 'Expand all'}
        </button>
      </div>
      <div className={styles.scroll}>
        <table className={styles.table}>
          <thead>
            <tr>
              {header.slice(0, split).map((cell, i) => (
                <th key={i}>{cell}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => {
              const expanded = open.has(i);
              const panel = `${id}-${i}`;
              return (
                <Fragment key={i}>
                  <tr
                    className={styles.summary}
                    data-open={expanded}
                    onClick={(event) => onRowClick(event, i)}>
                    {row.slice(0, split).map((cell, j) => (
                      <td key={j}>
                        {j === 0 && (
                          <button
                            type="button"
                            className={styles.chevron}
                            aria-expanded={expanded}
                            aria-controls={panel}
                            aria-label={`Show details for ${text(cell)}`}
                          />
                        )}
                        {cell}
                      </td>
                    ))}
                  </tr>
                  <tr className={styles.details}>
                    <td colSpan={split}>
                      <div id={panel} data-details={i} hidden={!expanded}>
                        {row[split]}
                      </div>
                    </td>
                  </tr>
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

const components = {table: Table};

export default function ExpandableTable({children}: {children: ReactNode}) {
  return <MDXProvider components={components}>{children}</MDXProvider>;
}
