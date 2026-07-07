export type ComponentState = 'default' | 'loading' | 'disabled' | 'empty' | 'error' | 'selected';

export interface MuseumSnippet {
  readonly importPath: string;
  readonly code: string;
}

export interface UiPreviewDescriptor {
  readonly id: string;
  readonly title: string;
  readonly group: 'basic' | 'media' | 'task' | 'space' | 'feedback';
  readonly states: readonly ComponentState[];
  readonly snippet: MuseumSnippet;
}

export function createSnippet(importName: string, packagePath: string): MuseumSnippet {
  return {
    importPath: packagePath,
    code: `import { ${importName} } from '${packagePath}';`,
  };
}
