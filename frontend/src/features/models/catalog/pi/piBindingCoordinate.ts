export function piBindingCoordinateKey(coordinate: {
  provider_id: string;
  model_id: string;
}): string {
  return JSON.stringify([coordinate.provider_id, coordinate.model_id]);
}
