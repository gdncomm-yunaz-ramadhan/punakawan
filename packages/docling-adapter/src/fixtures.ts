import type { DoclingConvertResponse } from './normalize.js';

/**
 * Fixture responses matching Docling Serve's real, documented
 * `/v1/convert/source` response shape (confirmed via Context7 for
 * `/docling-project/docling-serve`, docs/usage.md, and the DoclingDocument
 * model at docs/concepts/docling_document.md in `/docling-project/docling`).
 */

export const successResponse: DoclingConvertResponse = {
  document: {
    md_content: '# Title\n\nSome paragraph text.\n\n| A | B |\n| - | - |\n| 1 | 2 |\n',
    json_content: {
      schema_name: 'DoclingDocument',
      texts: [
        {
          label: 'section_header',
          text: 'Title',
          prov: [{ page_no: 1, bbox: { l: 0, t: 0, r: 100, b: 20 }, charspan: [0, 5] }],
        },
        {
          label: 'paragraph',
          text: 'Some paragraph text.',
          prov: [{ page_no: 1, bbox: { l: 0, t: 20, r: 100, b: 40 }, charspan: [6, 27] }],
        },
      ],
      tables: [
        {
          label: 'table',
          data: {
            num_rows: 2,
            num_cols: 2,
            table_cells: [
              { text: 'A', start_row_offset_idx: 0, start_col_offset_idx: 0 },
              { text: 'B', start_row_offset_idx: 0, start_col_offset_idx: 1 },
              { text: '1', start_row_offset_idx: 1, start_col_offset_idx: 0 },
              { text: '2', start_row_offset_idx: 1, start_col_offset_idx: 1 },
            ],
          },
          prov: [{ page_no: 1, bbox: { l: 0, t: 40, r: 100, b: 60 } }],
        },
      ],
    },
    html_content: null,
    text_content: null,
    doctags_content: null,
  },
  status: 'success',
  processing_time: 1.23,
  timings: {},
  errors: [],
};

export const partialSuccessResponse: DoclingConvertResponse = {
  document: {
    md_content: '# Partial\n\nOnly some of this converted.\n',
    json_content: {
      schema_name: 'DoclingDocument',
      texts: [
        {
          label: 'section_header',
          text: 'Partial',
          prov: [{ page_no: 1 }],
        },
      ],
      tables: [],
    },
    html_content: null,
    text_content: null,
    doctags_content: null,
  },
  status: 'partial_success',
  processing_time: 4.56,
  timings: {},
  errors: ['page 2: OCR timeout'],
};

export function jsonResponse(body: unknown, init: { status?: number } = {}): Response {
  const text = JSON.stringify(body);
  return new Response(text, {
    status: init.status ?? 200,
    headers: { 'Content-Type': 'application/json' },
  });
}
