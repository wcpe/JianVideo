const packageMinimums = new Map([
  ["github.com/wcpe/JianVideo", 5],
  ["github.com/wcpe/JianVideo/internal/smb", 25],
]);

export function minimumForPackage(pkg, defaultMinimum) {
  return packageMinimums.get(pkg) ?? defaultMinimum;
}
