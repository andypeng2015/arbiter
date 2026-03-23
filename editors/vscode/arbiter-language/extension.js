const cp = require("node:child_process");
const path = require("node:path");
const vscode = require("vscode");

const diagnosticPattern = /^(.*?):(\d+)(?::(\d+))?:\s+(.*)$/;

let lspClient = null;

function tryStartLSP(context) {
	const lspPath = vscode.workspace.getConfiguration("arbiter").get("lspPath", "arbiter-lsp");
	if (!lspPath) return false;

	try {
		// Test if the binary exists.
		cp.execFileSync(lspPath, ["--help"], { timeout: 2000, stdio: "ignore" });
	} catch {
		// Binary not found or doesn't support --help — that's fine, the LSP
		// runs on stdin/stdout and blocks, so just check it's executable.
		try {
			require("node:fs").accessSync(lspPath, require("node:fs").constants.X_OK);
		} catch {
			return false;
		}
	}

	const serverOptions = {
		command: lspPath,
		args: [],
		options: {},
	};
	const clientOptions = {
		documentSelector: [{ scheme: "file", language: "arbiter" }],
	};

	try {
		// vscode-languageclient may not be bundled — gracefully degrade.
		const { LanguageClient } = require("vscode-languageclient/node");
		lspClient = new LanguageClient("arbiter-lsp", "Arbiter Language Server", serverOptions, clientOptions);
		lspClient.start();
		context.subscriptions.push(lspClient);
		return true;
	} catch {
		return false;
	}
}

function activate(context) {
	// Try LSP first — if available, it handles diagnostics, completions, hover.
	const lspActive = tryStartLSP(context);
	if (lspActive) {
		// Register manual check command that just triggers a save (LSP re-validates).
		context.subscriptions.push(
			vscode.commands.registerCommand("arbiter.checkCurrentFile", () => {
				const editor = vscode.window.activeTextEditor;
				if (editor && isArbiterDocument(editor.document)) {
					editor.document.save();
				}
			})
		);
		return;
	}

	// Fallback: CLI-based diagnostics (existing behavior).
	const collection = vscode.languages.createDiagnosticCollection("arbiter");
	const ownedDiagnostics = new Map();
	let warnedMissingCLI = false;

	context.subscriptions.push(collection);

	const runCheck = async (document, manual) => {
		if (!isArbiterDocument(document) || document.isUntitled) {
			return;
		}

		const rootKey = document.uri.toString();
		clearOwnedDiagnostics(collection, ownedDiagnostics, rootKey);

		const cliPath = vscode.workspace.getConfiguration("arbiter", document.uri).get("cliPath", "arbiter");
		const cwd = workspaceDir(document.uri);

		let output;
		try {
			output = await execFile(cliPath, ["check", document.uri.fsPath], cwd);
		} catch (err) {
			if (err && err.code === "ENOENT") {
				if (!warnedMissingCLI) {
					warnedMissingCLI = true;
					vscode.window.showWarningMessage("Arbiter CLI not found in PATH. Set arbiter.cliPath to enable diagnostics.");
				}
				return;
			}
			output = `${err.stdout || ""}\n${err.stderr || ""}`.trim();
			if (!output) {
				output = String(err.message || err);
			}
		}

		const { grouped, hasDiagnostics } = parseDiagnostics(output, document.uri);
		const touched = [];
		for (const [key, diagnostics] of grouped.entries()) {
			const uri = vscode.Uri.parse(key);
			collection.set(uri, diagnostics);
			touched.push(key);
		}
		ownedDiagnostics.set(rootKey, touched);

		if (manual && !hasDiagnostics) {
			vscode.window.showInformationMessage(`Arbiter check passed: ${path.basename(document.uri.fsPath)}`);
		}
	};

	context.subscriptions.push(vscode.commands.registerCommand("arbiter.checkCurrentFile", async () => {
		const document = vscode.window.activeTextEditor && vscode.window.activeTextEditor.document;
		if (!document || !isArbiterDocument(document)) {
			return;
		}
		await runCheck(document, true);
	}));

	context.subscriptions.push(vscode.workspace.onDidOpenTextDocument(document => {
		if (shouldAutoCheck(document)) {
			void runCheck(document, false);
		}
	}));

	context.subscriptions.push(vscode.workspace.onDidSaveTextDocument(document => {
		if (shouldAutoCheck(document)) {
			void runCheck(document, false);
		}
	}));

	context.subscriptions.push(vscode.workspace.onDidCloseTextDocument(document => {
		if (!isArbiterDocument(document)) {
			return;
		}
		clearOwnedDiagnostics(collection, ownedDiagnostics, document.uri.toString());
	}));

	const active = vscode.window.activeTextEditor && vscode.window.activeTextEditor.document;
	if (shouldAutoCheck(active)) {
		void runCheck(active, false);
	}
}

function deactivate() {
	if (lspClient) {
		return lspClient.stop();
	}
}

function shouldAutoCheck(document) {
	if (!isArbiterDocument(document)) {
		return false;
	}
	return vscode.workspace.getConfiguration("arbiter", document.uri).get("runCheckOnSave", true);
}

function isArbiterDocument(document) {
	return !!document && document.languageId === "arbiter" && document.uri.scheme === "file";
}

function workspaceDir(uri) {
	const folder = vscode.workspace.getWorkspaceFolder(uri);
	if (folder) {
		return folder.uri.fsPath;
	}
	return path.dirname(uri.fsPath);
}

function clearOwnedDiagnostics(collection, ownedDiagnostics, rootKey) {
	const previous = ownedDiagnostics.get(rootKey);
	if (!previous) {
		return;
	}
	for (const uriKey of previous) {
		collection.delete(vscode.Uri.parse(uriKey));
	}
	ownedDiagnostics.delete(rootKey);
}

function execFile(command, args, cwd) {
	return new Promise((resolve, reject) => {
		cp.execFile(command, args, { cwd }, (error, stdout, stderr) => {
			const output = `${stdout || ""}\n${stderr || ""}`.trim();
			if (error) {
				error.stdout = stdout;
				error.stderr = stderr;
				reject(error);
				return;
			}
			resolve(output);
		});
	});
}

function parseDiagnostics(output, fallbackURI) {
	const grouped = new Map();
	let hasDiagnostics = false;
	const lines = String(output || "")
		.split(/\r?\n/)
		.map(line => line.trim())
		.filter(Boolean);

	for (const line of lines) {
		if (line.endsWith(": ok")) {
			continue;
		}
		const match = line.match(diagnosticPattern);
		if (!match) {
			appendDiagnostic(grouped, fallbackURI, 0, 0, line);
			hasDiagnostics = true;
			continue;
		}

		const file = match[1];
		const lineNumber = Math.max(parseInt(match[2], 10), 1);
		const column = match[3] ? Math.max(parseInt(match[3], 10), 1) : 1;
		appendDiagnostic(grouped, vscode.Uri.file(file), lineNumber-1, column-1, match[4]);
		hasDiagnostics = true;
	}

	return { grouped, hasDiagnostics };
}

function appendDiagnostic(grouped, uri, line, column, message) {
	const key = uri.toString();
	const diagnostics = grouped.get(key) || [];
	const start = new vscode.Position(line, Math.max(column, 0));
	const end = new vscode.Position(line, Math.max(column, 0) + 1);
	diagnostics.push(new vscode.Diagnostic(new vscode.Range(start, end), message, vscode.DiagnosticSeverity.Error));
	grouped.set(key, diagnostics);
}

module.exports = {
	activate,
	deactivate,
};
