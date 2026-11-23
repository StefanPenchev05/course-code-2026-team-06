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

/** Catalog lists are wrapped in {"data": [...]} without pagination. */
export interface DataList<T> {
  data: T[];
}
