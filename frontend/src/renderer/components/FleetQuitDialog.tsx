import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./ui/dialog";

export function FleetQuitDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
	const { t } = useTranslation();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const quit = async () => {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			if (!window.ao?.menu) throw new Error("Fleet desktop is unavailable.");
			await window.ao.menu.action("fleet.quit");
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally { setBusy(false); }
	};
	return <Dialog open={open} onOpenChange={(next) => { if (!busy) { setError(null); onOpenChange(next); } }}>
		<DialogContent showCloseButton={!busy} data-browser-native-overlay="true" onEscapeKeyDown={(event) => { if (busy) event.preventDefault(); }} onPointerDownOutside={(event) => { if (busy) event.preventDefault(); }}>
			<DialogHeader>
				<DialogTitle>{t("fleet.quitTitle")}</DialogTitle>
				<DialogDescription>{t("fleet.quitDescription")}</DialogDescription>
			</DialogHeader>
			{busy && <p role="status" className="text-sm text-muted-foreground">{t("fleet.quitting")}</p>}
			{error && <div role="alert" className="text-sm text-destructive"><p>{t("fleet.quitFailed")}</p><p className="mt-2 break-words whitespace-pre-wrap">{error}</p></div>}
			<DialogFooter>
				<Button variant="outline" disabled={busy} onClick={() => { setError(null); onOpenChange(false); }}>{t("confirm.cancel")}</Button>
				<Button disabled={busy} onClick={() => void quit()}>{t("fleet.quitConfirm")}</Button>
			</DialogFooter>
		</DialogContent>
	</Dialog>;
}
