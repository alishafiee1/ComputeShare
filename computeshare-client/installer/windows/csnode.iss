; Inno Setup script — node card 003. Build: iscc csnode.iss (unsigned: SmartScreen warning documented)
#define Ver GetEnv("VERSION")
#if Ver == ""
  #error VERSION is not set (example: set VERSION=0.2.0 and run iscc csnode.iss)
#endif
[Setup]
AppName=ComputeShare Node
AppVersion={#Ver}
DefaultDirName={autopf}\ComputeShare
PrivilegesRequired=admin
OutputBaseFilename=csnode-setup-{#Ver}
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.19044
[Files]
Source: "..\..\dist\csnode-windows-amd64.exe"; DestDir: "{app}"; DestName: "csnode.exe"
Source: "..\..\dist\cs-windows-amd64.exe"; DestDir: "{app}"; DestName: "cs.exe"
Source: "csnode-linux-rootfs.tar.gz"; DestDir: "{app}"
Source: "SHA256SUMS"; DestDir: "{app}"
Source: "SHA256SUMS.sig"; DestDir: "{app}"
[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath
[Run]
Filename: "{app}\csnode.exe"; Parameters: "install --rootfs ""{app}\csnode-linux-rootfs.tar.gz"""; Flags: waituntilterminated
[UninstallRun]
Filename: "{app}\csnode.exe"; Parameters: "uninstall"; Flags: waituntilterminated; RunOnceId: "csnodepurge"
[Code]
function NeedsAddPath(): Boolean;
var p: string;
begin
  if not RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', p) then begin Result := True; exit; end;
  Result := Pos(';' + ExpandConstant('{app}') + ';', ';' + p + ';') = 0;
end;
