; RUNTIDE installer (Inno Setup 6). Per-user by default: no administrator rights.
#ifndef AppVersion
  #define AppVersion "0.9.1"
#endif

[Setup]
AppId={{B7C1A4F2-6D2E-4B5C-9F0A-52554E544944}
AppName=RUNTIDE
AppVersion={#AppVersion}
AppVerName=RUNTIDE {#AppVersion}
AppPublisher=RUNTIDE contributors
DefaultDirName={autopf}\RUNTIDE
DefaultGroupName=RUNTIDE
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=RUNTIDE-{#AppVersion}-setup
SetupIconFile=windows\icon.ico
UninstallDisplayIcon={app}\RUNTIDE-Desktop.exe
WizardStyle=modern
Compression=lzma2
SolidCompression=yes
LicenseFile=..\LICENSE
; Closes a running RUNTIDE during upgrade instead of failing on locked files.
CloseApplications=yes
RestartApplications=no
MinVersion=10.0.19045

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked
Name: "addtopath"; Description: "Add the runtide command to my PATH (per-user)"; Flags: unchecked

[Files]
Source: "..\dist\RUNTIDE-Desktop.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\runtide.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\runtide-mcp.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\RUNTIDE"; Filename: "{app}\RUNTIDE-Desktop.exe"
Name: "{group}\Uninstall RUNTIDE"; Filename: "{uninstallexe}"
Name: "{autodesktop}\RUNTIDE"; Filename: "{app}\RUNTIDE-Desktop.exe"; Tasks: desktopicon

[Registry]
; Only when the user ticks the PATH task. Uses the per-user environment, never the machine one.
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: addtopath; Check: NeedsAddPath(ExpandConstant('{app}'))

[Run]
Filename: "{app}\RUNTIDE-Desktop.exe"; Description: "Launch RUNTIDE"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; Stop the background engine (and the services it manages) before files are removed.
Filename: "{app}\runtide.exe"; Parameters: "daemon stop"; Flags: runhidden skipifdoesntexist; RunOnceId: "StopRuntideEngine"

[UninstallDelete]
; User data (%LOCALAPPDATA%\Runtide: database, logs, backups) is intentionally kept across uninstall and upgrade.

[Code]
function NeedsAddPath(Param: string): Boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(OrigPath) + ';') = 0;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Path: string;
  P: Integer;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    // Remove only the entry this installer may have added.
    if RegQueryStringValue(HKCU, 'Environment', 'Path', Path) then
    begin
      P := Pos(';' + ExpandConstant('{app}'), Path);
      if P > 0 then
      begin
        Delete(Path, P, Length(ExpandConstant('{app}')) + 1);
        RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Path);
      end;
    end;
    // Remove the opt-in startup entry if the user had enabled it.
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'RUNTIDE');
  end;
end;
