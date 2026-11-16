import { SettingsOptionMenu } from "./SettingsOptionMenu";

export type ServiceTier = "default" | "priority";
export type ModelParameterCapabilities = {
 supportsFast?: boolean;
};
export function supportsFast(model?: ModelParameterCapabilities): boolean {
 return model?.supportsFast ?? false;
}
// Unknown custom models must not borrow another model's capabilities.
export function findParameterModel<T extends ModelParameterCapabilities & { id: string; isDefault?: boolean }>(models: T[] | undefined, model: string): T | undefined {
 return model ? models?.find((item) => item.id === model) : models?.find((item) => item.isDefault);
}
export function resolveServiceTier(model: ModelParameterCapabilities | undefined, value?: string): ServiceTier {
 return supportsFast(model) && value === "priority" ? "priority" : "default";
}
export function ServiceTierControl({ model, value, onChange, disabled = false, ariaLabel = "Fast" }: {
 ariaLabel?: string;
 model?: ModelParameterCapabilities;
 value?: string;
 onChange: (value: ServiceTier) => void;
 disabled?: boolean;
}) {
 if (!supportsFast(model)) return null;
 return <SettingsOptionMenu<ServiceTier>
  aria-label={ariaLabel} value={resolveServiceTier(model, value)}
  options={[{ value: "default", label: "Fast off" }, { value: "priority", label: "Fast on" }]}
  onChange={onChange} disabled={disabled}
 />;
}
