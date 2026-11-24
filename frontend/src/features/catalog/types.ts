/** Shapes returned by the Catalog API (backend/internal/catalog/domain/catalog.go). */

export type DegreeLevel = 'BACHELOR' | 'MASTER';

export interface Faculty {
  id: string;
  name: string;
}

export interface Program {
  id: string;
  faculty_id: string;
  name: string;
  level: DegreeLevel;
}

export interface Subject {
  id: string;
  code: string;
  name: string;
}

/** Faculty and program lists are wrapped in {"data": [...]} without pagination; subject lists are paginated. */
export interface DataList<T> {
  data: T[];
}
